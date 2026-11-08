"""
OVHcloud Public SKU Change Watcher
==================================
Monitors the public /dedicated/server/datacenter/availabilities endpoint for
newly added or removed server SKUs (including racked-but-unavailable models)
with anti-false-positive protection:
  1. Initial run establishes baseline without alerting.
  2. SKU removal requires 2 consecutive missing checks to confirm.
  3. If total SKU count drops by >30% in a single check, the round is skipped as an API glitch.
  4. Supports product-line filtering (KS/RISE/SYS/ADV/SCALE/HGR), regional variant merging,
     and optional configuration-level (FQN) change tracking.
"""

from __future__ import annotations

from datetime import datetime, timezone, timedelta
import json
import os
import threading
import time
from typing import Any, Callable, Dict, List, Optional, Set, Tuple
import urllib.request

try:
    import requests as _requests
except ImportError:
    _requests = None

from catalog_service import (
    ENDPOINT_BASE_URLS,
    CatalogService,
    catalog_service as default_catalog_service,
    format_cpu,
    format_memory,
    format_storage,
    parse_memory_code,
    parse_plan_region,
    parse_storage_code,
)
from notifications.events import EVENT_SKU_CHANGED, NotificationEvent

DATA_DIR = os.path.join(os.path.dirname(os.path.abspath(__file__)), "data")
os.makedirs(DATA_DIR, exist_ok=True)

MAX_CHANGE_HISTORY = 200


def _now_iso() -> str:
    tz = timezone(timedelta(hours=8))
    return datetime.now(tz).isoformat(timespec="seconds")


def _now_display() -> str:
    tz = timezone(timedelta(hours=8))
    return datetime.now(tz).strftime("%Y-%m-%d %H:%M:%S")


class SkuWatcher:
    """Background watcher and diff engine for OVHcloud public server SKUs."""

    def __init__(
        self,
        get_config_func: Callable[[], Dict[str, Any]],
        dispatch_event_func: Callable[[NotificationEvent], bool],
        add_log_func: Callable[[str, str, str], None] = lambda l, m, c="sku": None,
        catalog_svc: CatalogService = default_catalog_service,
        data_dir: str = DATA_DIR,
    ):
        self._get_config = get_config_func
        self._dispatch_event = dispatch_event_func
        self._add_log = add_log_func
        self._catalog_svc = catalog_svc
        self.data_dir = data_dir
        os.makedirs(self.data_dir, exist_ok=True)

        self.snapshot_file = os.path.join(self.data_dir, "sku_snapshot.json")
        self.changes_file = os.path.join(self.data_dir, "sku_changes.json")

        self._lock = threading.Lock()
        self.running = False
        self.thread: Optional[threading.Thread] = None
        self.last_check_time: Optional[str] = None
        self.last_error: Optional[str] = None

    def _load_snapshot(self) -> Dict[str, Any]:
        if os.path.exists(self.snapshot_file):
            try:
                with open(self.snapshot_file, "r", encoding="utf-8") as f:
                    return json.load(f)
            except Exception:
                pass
        return {"initialized": False, "endpoint": "", "skus": {}, "missingCounts": {}}

    def _save_snapshot(self, snap: Dict[str, Any]) -> None:
        try:
            with open(self.snapshot_file, "w", encoding="utf-8") as f:
                json.dump(snap, f, ensure_ascii=False, indent=2)
        except Exception as e:
            self._add_log("ERROR", f"保存 SKU 快照失败: {e}", "sku")

    def get_changes(self, limit: int = MAX_CHANGE_HISTORY) -> List[Dict[str, Any]]:
        if os.path.exists(self.changes_file):
            try:
                with open(self.changes_file, "r", encoding="utf-8") as f:
                    items = json.load(f)
                if isinstance(items, list):
                    return items[:limit]
            except Exception:
                pass
        return []

    def _append_changes(self, new_entries: List[Dict[str, Any]]) -> None:
        if not new_entries:
            return
        existing = self.get_changes(limit=MAX_CHANGE_HISTORY)
        combined = (new_entries + existing)[:MAX_CHANGE_HISTORY]
        try:
            with open(self.changes_file, "w", encoding="utf-8") as f:
                json.dump(combined, f, ensure_ascii=False, indent=2)
        except Exception as e:
            self._add_log("ERROR", f"保存 SKU 变动记录失败: {e}", "sku")

    def _get_rules(self) -> Tuple[str, str, Dict[str, Any]]:
        cfg = self._get_config() or {}
        endpoint = (cfg.get("endpoint") or "ovh-eu").strip()
        zone = (cfg.get("zone") or "IE").strip()
        notif = cfg.get("notifications") or {}
        rules = notif.get("rules") or {}
        return endpoint, zone, {
            "skuEnabled": bool(rules.get("skuEnabled", True)),
            "skuInterval": max(60, int(rules.get("skuInterval") or 600)),
            "skuTrackConfigs": bool(rules.get("skuTrackConfigs", False)),
            "skuRanges": [
                str(x).upper()
                for x in (rules.get("skuRanges") or ["KS", "RISE", "SYS", "ADV", "SCALE", "HGR"])
            ],
            "skuMergeRegional": bool(rules.get("skuMergeRegional", True)),
        }

    def fetch_public_availabilities(self, endpoint: str = "ovh-eu") -> List[Dict[str, Any]]:
        base_url = ENDPOINT_BASE_URLS.get(endpoint, ENDPOINT_BASE_URLS["ovh-eu"])
        url = f"{base_url}/dedicated/server/datacenter/availabilities"
        if _requests is not None:
            resp = _requests.get(url, timeout=30)
            resp.raise_for_status()
            return resp.json()
        req = urllib.request.Request(url, headers={"Accept": "application/json"})
        with urllib.request.urlopen(req, timeout=30) as r:
            return json.loads(r.read().decode("utf-8"))

    def aggregate_skus(
        self,
        raw_items: List[Dict[str, Any]],
        endpoint: str = "ovh-eu",
        zone: str = "IE",
        allowed_ranges: Optional[List[str]] = None,
        merge_regional: bool = True,
        dictionary: Optional[Dict[str, Any]] = None,
    ) -> Dict[str, Dict[str, Any]]:
        """
        Group raw availability items by SKU key (base planCode if merge_regional=True,
        else exact planCode) and summarize friendly metadata, configs, and available DCs.
        """
        cat_dict = (
            dictionary
            if dictionary is not None
            else self._catalog_svc.get_dictionary(endpoint=endpoint, zone=zone)
        )
        allowed_set = {r.upper() for r in allowed_ranges} if allowed_ranges else None

        grouped: Dict[str, Dict[str, Any]] = {}
        for item in raw_items or []:
            if not isinstance(item, dict):
                continue
            plan_code = (item.get("planCode") or "").strip()
            if not plan_code:
                continue
            server_code = (item.get("server") or "").strip()
            base_code, region_info = parse_plan_region(plan_code)
            sku_key = (base_code or plan_code) if merge_regional else plan_code

            meta = self._catalog_svc.resolve_plan(
                plan_code,
                server_code=server_code,
                endpoint=endpoint,
                zone=zone,
                dictionary=cat_dict,
            )
            prod_range = (meta.get("range") or "OTHER").upper()
            if allowed_set and prod_range not in allowed_set and "OTHER" not in allowed_set:
                continue

            entry = grouped.get(sku_key)
            if not entry:
                entry = {
                    "skuKey": sku_key,
                    "planCodes": set(),
                    "name": meta.get("name") or sku_key,
                    "range": prod_range,
                    "cpu": meta.get("cpu"),
                    "cpuText": format_cpu(meta.get("cpu"), "zh-CN"),
                    "memories": set(),
                    "storages": set(),
                    "configs": set(),
                    "availableDcs": set(),
                    "regions": set(),
                }
                grouped[sku_key] = entry

            entry["planCodes"].add(plan_code)
            if region_info and region_info.get("code"):
                entry["regions"].add(region_info["code"])

            mem_raw = item.get("memory") or ""
            sto_raw = item.get("storage") or ""
            if mem_raw:
                entry["memories"].add(mem_raw)
            if sto_raw:
                entry["storages"].add(sto_raw)
            if mem_raw or sto_raw:
                entry["configs"].add(f"{mem_raw}|{sto_raw}")

            for dc_item in item.get("datacenters") or []:
                avail = (dc_item.get("availability") or "").lower()
                dc_code = (dc_item.get("datacenter") or "").upper()
                if dc_code and avail not in ("unavailable", "unknown", ""):
                    entry["availableDcs"].add(dc_code)

        # Convert sets to sorted lists for JSON serialization
        serialized: Dict[str, Dict[str, Any]] = {}
        for k, v in grouped.items():
            mem_list = sorted(v["memories"])
            sto_list = sorted(v["storages"])
            mem_friendly = [format_memory(parse_memory_code(m), "zh-CN") for m in mem_list[:4]]
            sto_friendly = [format_storage(parse_storage_code(s), "zh-CN") for s in sto_list[:4]]
            serialized[k] = {
                "skuKey": k,
                "planCodes": sorted(v["planCodes"]),
                "name": v["name"],
                "range": v["range"],
                "cpu": v["cpu"],
                "cpuText": v["cpuText"],
                "memories": mem_list,
                "storages": sto_list,
                "memorySummary": " / ".join(mem_friendly) + (" 等" if len(mem_list) > 4 else ""),
                "storageSummary": " / ".join(sto_friendly) + (" 等" if len(sto_list) > 4 else ""),
                "configs": sorted(v["configs"]),
                "availableDcs": sorted(v["availableDcs"]),
                "regions": sorted(v["regions"]),
            }
        return serialized

    def check_once(
        self,
        raw_items: Optional[List[Dict[str, Any]]] = None,
        dictionary: Optional[Dict[str, Any]] = None,
    ) -> Dict[str, Any]:
        """
        Execute one SKU diff check cycle.
        Returns summary dict:
          {
            "status": "baseline_initialized" | "skipped_glitch" | "ok",
            "added": [...],
            "removed": [...],
            "configChanged": [...],
            "totalSkus": int
          }
        """
        with self._lock:
            endpoint, zone, rules = self._get_rules()
            if raw_items is None:
                try:
                    raw_items = self.fetch_public_availabilities(endpoint=endpoint)
                except Exception as e:
                    self.last_error = str(e)
                    self._add_log("ERROR", f"拉取公开库存 API 失败: {e}", "sku")
                    return {"status": "error", "error": str(e), "added": [], "removed": [], "configChanged": []}

            current_skus = self.aggregate_skus(
                raw_items,
                endpoint=endpoint,
                zone=zone,
                allowed_ranges=rules["skuRanges"],
                merge_regional=rules["skuMergeRegional"],
                dictionary=dictionary,
            )
            self.last_check_time = _now_iso()
            self.last_error = None

            snap = self._load_snapshot()
            prev_skus: Dict[str, Dict[str, Any]] = snap.get("skus") or {}
            missing_counts: Dict[str, int] = snap.get("missingCounts") or {}

            # Rule 1: First run or endpoint changed -> initialize baseline without notifying
            if not snap.get("initialized") or snap.get("endpoint") != endpoint or not prev_skus:
                now_ts = _now_iso()
                for k, v in current_skus.items():
                    v["firstSeenAt"] = now_ts
                new_snap = {
                    "initialized": True,
                    "endpoint": endpoint,
                    "updatedAt": now_ts,
                    "skus": current_skus,
                    "missingCounts": {},
                }
                self._save_snapshot(new_snap)
                self._add_log(
                    "INFO",
                    f"SKU 监控基线初始化完成 ({endpoint})，共记录 {len(current_skus)} 个型号",
                    "sku",
                )
                return {
                    "status": "baseline_initialized",
                    "added": [],
                    "removed": [],
                    "configChanged": [],
                    "totalSkus": len(current_skus),
                }

            # Rule 3: Anti-glitch check — if total SKU count drops by >30%, skip round
            prev_count = len(prev_skus)
            curr_count = len(current_skus)
            if prev_count >= 10 and curr_count < prev_count * 0.7:
                self._add_log(
                    "WARNING",
                    f"SKU 数量骤降 ({prev_count} -> {curr_count}，降幅 >30%)，判定为 API 瞬时异常，跳过本轮对比",
                    "sku",
                )
                return {
                    "status": "skipped_glitch",
                    "added": [],
                    "removed": [],
                    "configChanged": [],
                    "totalSkus": curr_count,
                }

            now_ts = _now_iso()
            added_list: List[Dict[str, Any]] = []
            removed_list: List[Dict[str, Any]] = []
            config_changed_list: List[Dict[str, Any]] = []

            updated_skus: Dict[str, Dict[str, Any]] = {}
            new_missing_counts: Dict[str, int] = {}

            # Detect newly added SKUs and optional config changes
            for sku_key, curr_info in current_skus.items():
                if sku_key not in prev_skus:
                    curr_info["firstSeenAt"] = now_ts
                    added_list.append(curr_info)
                    updated_skus[sku_key] = curr_info
                else:
                    prev_info = prev_skus[sku_key]
                    curr_info["firstSeenAt"] = prev_info.get("firstSeenAt") or now_ts
                    if rules.get("skuTrackConfigs"):
                        old_cfgs = set(prev_info.get("configs") or [])
                        new_cfgs = set(curr_info.get("configs") or [])
                        added_cfgs = sorted(new_cfgs - old_cfgs)
                        removed_cfgs = sorted(old_cfgs - new_cfgs)
                        if added_cfgs or removed_cfgs:
                            config_changed_list.append(
                                {
                                    "skuKey": sku_key,
                                    "name": curr_info["name"],
                                    "range": curr_info["range"],
                                    "cpuText": curr_info.get("cpuText") or "",
                                    "addedConfigs": [self._format_cfg_pair(c) for c in added_cfgs],
                                    "removedConfigs": [self._format_cfg_pair(c) for c in removed_cfgs],
                                }
                            )
                    updated_skus[sku_key] = curr_info

            # Rule 2: Removal requires 2 consecutive missing checks
            for sku_key, prev_info in prev_skus.items():
                if sku_key not in current_skus:
                    cnt = int(missing_counts.get(sku_key, 0)) + 1
                    if cnt >= 2:
                        removed_list.append(prev_info)
                    else:
                        new_missing_counts[sku_key] = cnt
                        updated_skus[sku_key] = prev_info

            # Save updated snapshot
            self._save_snapshot(
                {
                    "initialized": True,
                    "endpoint": endpoint,
                    "updatedAt": now_ts,
                    "skus": updated_skus,
                    "missingCounts": new_missing_counts,
                }
            )

            # Record changes to timeline & dispatch digest notification
            change_records: List[Dict[str, Any]] = []
            for item in added_list:
                dcs = item.get("availableDcs") or []
                stock_state = f"已上架 · {'/'.join(dcs)} 有货" if dcs else "已上架 · 暂未开售"
                change_records.append(
                    {
                        "id": f"add-{item['skuKey']}-{int(time.time())}",
                        "type": "added",
                        "timestamp": now_ts,
                        "skuKey": item["skuKey"],
                        "planCodes": item.get("planCodes") or [item["skuKey"]],
                        "name": item["name"],
                        "range": item["range"],
                        "cpuText": item.get("cpuText") or "",
                        "memorySummary": item.get("memorySummary") or "",
                        "storageSummary": item.get("storageSummary") or "",
                        "availableDcs": dcs,
                        "stockState": stock_state,
                    }
                )

            for item in removed_list:
                change_records.append(
                    {
                        "id": f"rem-{item['skuKey']}-{int(time.time())}",
                        "type": "removed",
                        "timestamp": now_ts,
                        "skuKey": item["skuKey"],
                        "planCodes": item.get("planCodes") or [item["skuKey"]],
                        "name": item.get("name") or item["skuKey"],
                        "range": item.get("range") or "OTHER",
                        "cpuText": item.get("cpuText") or "",
                        "memorySummary": item.get("memorySummary") or "",
                        "storageSummary": item.get("storageSummary") or "",
                        "availableDcs": [],
                        "stockState": "已从货架移除",
                    }
                )

            for item in config_changed_list:
                change_records.append(
                    {
                        "id": f"cfg-{item['skuKey']}-{int(time.time())}",
                        "type": "config_changed",
                        "timestamp": now_ts,
                        "skuKey": item["skuKey"],
                        "planCodes": [item["skuKey"]],
                        "name": item["name"],
                        "range": item["range"],
                        "cpuText": item.get("cpuText") or "",
                        "addedConfigs": item["addedConfigs"],
                        "removedConfigs": item["removedConfigs"],
                        "stockState": "配置组合更新",
                    }
                )

            if change_records:
                self._append_changes(change_records)
                self._send_digest(endpoint, added_list, removed_list, config_changed_list)

            return {
                "status": "ok",
                "added": added_list,
                "removed": removed_list,
                "configChanged": config_changed_list,
                "totalSkus": len(updated_skus),
            }

    @staticmethod
    def _format_cfg_pair(pair_str: str) -> str:
        parts = pair_str.split("|", 1)
        mem = format_memory(parse_memory_code(parts[0]), "zh-CN") if parts and parts[0] else ""
        sto = format_storage(parse_storage_code(parts[1]), "zh-CN") if len(parts) > 1 and parts[1] else ""
        return " + ".join([x for x in (mem, sto) if x]) or pair_str

    def _send_digest(
        self,
        endpoint: str,
        added: List[Dict[str, Any]],
        removed: List[Dict[str, Any]],
        config_changed: List[Dict[str, Any]],
    ) -> None:
        lines = [f"📦 OVHcloud ({endpoint.upper()}) 货架 SKU 变动摘要"]
        fields = [
            {"label": "API 区域", "value": endpoint.upper()},
            {
                "label": "变动统计",
                "value": f"上新 {len(added)} 款 · 下架 {len(removed)} 款"
                + (f" · 配置变动 {len(config_changed)} 款" if config_changed else ""),
            },
        ]

        if added:
            lines.append(f"\n🆕 新增上架型号 ({len(added)}):")
            for item in added:
                dcs = item.get("availableDcs") or []
                stock_tag = f"🟢 {'/'.join(dcs)} 有货" if dcs else "⚪ 已上架·暂未开售"
                cpu_part = f" | {item['cpuText']}" if item.get("cpuText") else ""
                lines.append(f"• {item['name']} ({item['skuKey']}){cpu_part}")
                if item.get("memorySummary"):
                    lines.append(f"  ├─ 内存: {item['memorySummary']}")
                if item.get("storageSummary"):
                    lines.append(f"  ├─ 存储: {item['storageSummary']}")
                lines.append(f"  └─ 状态: {stock_tag}")

        if removed:
            lines.append(f"\n📤 下架移除型号 ({len(removed)}):")
            for item in removed:
                cpu_part = f" | {item['cpuText']}" if item.get("cpuText") else ""
                lines.append(f"• {item.get('name', item['skuKey'])} ({item['skuKey']}){cpu_part}")

        if config_changed:
            lines.append(f"\n🔧 配置组合变动 ({len(config_changed)}):")
            for item in config_changed:
                lines.append(f"• {item['name']} ({item['skuKey']})")
                for c in item.get("addedConfigs") or []:
                    lines.append(f"  + 新增配置: {c}")
                for c in item.get("removedConfigs") or []:
                    lines.append(f"  - 移除配置: {c}")

        lines.append(f"\n🕒 检测时间: {_now_display()}")

        title = f"SKU 变动提醒：上新 {len(added)} 款 / 下架 {len(removed)} 款"
        event = NotificationEvent(
            event_type=EVENT_SKU_CHANGED,
            title=title,
            plain_text="\n".join(lines),
            fields=fields,
        )
        self._dispatch_event(event)
        self._add_log("INFO", f"已推送 SKU 变动摘要: {title}", "sku")

    def get_status(self) -> Dict[str, Any]:
        snap = self._load_snapshot()
        _, _, rules = self._get_rules()
        skus = snap.get("skus") or {}
        # Build recentAdded map (planCode -> firstSeenAt) for models added within 7 days
        now_dt = datetime.now(timezone(timedelta(hours=8)))
        recent_codes: Dict[str, str] = {}
        baseline_time = snap.get("updatedAt")
        for sku_key, info in skus.items():
            first_seen = info.get("firstSeenAt")
            if not first_seen:
                continue
            try:
                dt = datetime.fromisoformat(first_seen)
                if (now_dt - dt).total_seconds() <= 7 * 86400:
                    recent_codes[sku_key] = first_seen
                    for pc in info.get("planCodes") or []:
                        recent_codes[pc] = first_seen
            except Exception:
                pass

        return {
            "running": self.running,
            "initialized": bool(snap.get("initialized")),
            "endpoint": snap.get("endpoint") or "ovh-eu",
            "totalSkus": len(skus),
            "lastCheckTime": self.last_check_time or baseline_time,
            "lastError": self.last_error,
            "rules": rules,
            "recentFirstSeen": recent_codes,
        }

    def _loop(self) -> None:
        self._add_log("INFO", "SKU 变动监控循环已启动", "sku")
        while self.running:
            try:
                _, _, rules = self._get_rules()
                if rules.get("skuEnabled", True):
                    self.check_once()
                interval = max(60, int(rules.get("skuInterval") or 600))
            except Exception as e:
                self._add_log("ERROR", f"SKU 监控循环异常: {e}", "sku")
                interval = 600

            for _ in range(interval):
                if not self.running:
                    break
                time.sleep(1)
        self._add_log("INFO", "SKU 变动监控循环已停止", "sku")

    def start(self) -> bool:
        if self.running:
            return False
        self.running = True
        self.thread = threading.Thread(target=self._loop, daemon=True)
        self.thread.start()
        return True

    def stop(self) -> bool:
        if not self.running:
            return False
        self.running = False
        return True
