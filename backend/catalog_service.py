"""
OVHcloud Public Catalog & Hardware Spec Dictionary Service
==========================================================
Fetches OVHcloud public Eco + Baremetal catalogs (no API key required),
caches the merged dictionary on disk (12h TTL), and provides a deterministic
local parser fallback for all memory, storage, systemStorage, GPU, and
planCode strings.
"""

from __future__ import annotations

import hashlib
import json
import os
import re
import threading
import time
from typing import Any, Dict, List, Optional, Tuple
import urllib.request

try:
    import requests as _requests
except ImportError:
    _requests = None

CACHE_DIR = os.path.join(os.path.dirname(os.path.abspath(__file__)), "cache")
os.makedirs(CACHE_DIR, exist_ok=True)

CATALOG_TTL_SECONDS = 12 * 3600  # 12 hours

ENDPOINT_BASE_URLS = {
    "ovh-eu": "https://eu.api.ovh.com/v1",
    "ovh-ca": "https://ca.api.ovh.com/v1",
    "ovh-us": "https://api.us.ovhcloud.com/v1",
}

REGION_SUFFIX_MAP = {
    "sgp": {"code": "SGP", "zh-CN": "新加坡", "en": "Singapore", "fr": "Singapour"},
    "syd": {"code": "SYD", "zh-CN": "悉尼", "en": "Sydney", "fr": "Sydney"},
    "mum": {"code": "MUM", "zh-CN": "孟买", "en": "Mumbai", "fr": "Mumbai"},
    "apac": {"code": "APAC", "zh-CN": "亚太", "en": "APAC", "fr": "APAC"},
}


def format_capacity_gb(cap_gb: int, unit_gb: str = "GB", unit_tb: str = "TB") -> str:
    """Convert GB integer into human-friendly GB or TB (for >= 1000 GB)."""
    if cap_gb <= 0:
        return f"0 {unit_gb}"
    if cap_gb >= 1000:
        if cap_gb % 1000 == 0:
            return f"{cap_gb // 1000} {unit_tb}"
        tb_val = f"{cap_gb / 1000:.2f}".rstrip("0").rstrip(".")
        return f"{tb_val} {unit_tb}"
    return f"{cap_gb} {unit_gb}"


def parse_plan_region(plan_code: str) -> Tuple[str, Optional[Dict[str, str]]]:
    """
    Extract base planCode (without -v1/-v2/-sgp/-syd/-mum) and optional region tag.
    Returns (base_code, region_info_or_None).
    """
    if not plan_code:
        return "", None
    code = plan_code.strip()
    region_info = None
    m_reg = re.search(r"-(sgp|syd|mum|apac)$", code, re.IGNORECASE)
    if m_reg:
        reg_key = m_reg.group(1).lower()
        region_info = REGION_SUFFIX_MAP.get(reg_key)
        code = code[: m_reg.start()]
    # Strip version suffix like -v1, -v2, -v3 or trailing version digit on 24ska012
    base_code = re.sub(r"-v\d+$", "", code, flags=re.IGNORECASE)
    return base_code, region_info


def infer_plan_from_code(plan_code: str) -> Dict[str, Any]:
    """
    Deterministic fallback parser for OVH planCode / server code when not yet
    present in the public catalog (e.g. newly racked 27skle01-v1, 27risele01-v1).
    """
    base_code, region_info = parse_plan_region(plan_code)
    # Also handle trailing single version digit like 24ska012 -> 24ska01
    clean = re.sub(r"^(\d{2}[a-z]+\d{2})\d$", r"\1", base_code.lower())

    m = re.match(r"^(\d{2})([a-z0-9-]+)$", clean)
    body = m.group(2) if m else clean

    name = plan_code.upper()
    product_range = "other"

    # Match product line patterns
    if body.startswith("ska"):
        idx = body[3:].lstrip("0") or "A"
        name = "KS-A" if body == "ska01" else f"KS-A{idx}"
        product_range = "KS"
    elif body.startswith("skb"):
        idx = body[3:].lstrip("0")
        name = "KS-B" if body == "skb01" else f"KS-B{idx}"
        product_range = "KS"
    elif body.startswith("skc"):
        idx = body[3:].lstrip("0")
        name = "KS-C" if body == "skc01" else f"KS-C{idx}"
        product_range = "KS"
    elif body.startswith("skle"):
        num = body[4:].lstrip("0") or "1"
        name = f"KS-LE-{num}"
        product_range = "KS"
    elif body.startswith("skgame"):
        num = body[6:].lstrip("0") or "1"
        name = f"KS-GAME-{num}"
        product_range = "KS"
    elif body.startswith("skstor"):
        num = body[6:].lstrip("0") or "1"
        name = f"KS-STOR-{num}"
        product_range = "KS"
    elif body.startswith("sk"):
        rest = body[2:]
        m_num = re.match(r"^(\d+)([a-z]*)$", rest)
        if m_num:
            n = int(m_num.group(1))
            suffix = m_num.group(2).upper()
            if n >= 10 and n % 10 == 0:
                n = n // 10
            name = f"KS-{n}{('-' + suffix) if suffix else ''}"
        product_range = "KS"
    elif body.startswith("risele"):
        num = body[6:].lstrip("0") or "1"
        name = f"RISE-LE-{num}"
        product_range = "RISE"
    elif body.startswith("risegame"):
        num = body[8:].lstrip("0") or "1"
        name = f"RISE-GAME-{num}"
        product_range = "RISE"
    elif body.startswith("risestor"):
        num = body[8:].lstrip("0") or "1"
        name = "RISE-STOR" if num == "1" else f"RISE-STOR-{num}"
        product_range = "RISE"
    elif body.startswith("rise"):
        rest = body[4:]
        m_sz = re.match(r"^(s|m|l|xl)(\d+)$", rest)
        if m_sz:
            name = f"RISE-{m_sz.group(1).upper()}"
        else:
            num = rest.lstrip("0") or "1"
            name = f"RISE-{num}"
        product_range = "RISE"
    elif body.startswith("sysstorle"):
        num = body[9:].lstrip("0") or "1"
        name = f"SYS-STOR-LE-{num}"
        product_range = "SYS"
    elif body.startswith("sysle"):
        num = body[5:].lstrip("0") or "1"
        name = f"SYS-LE-{num}"
        product_range = "SYS"
    elif body.startswith("sysgame"):
        num = body[7:].lstrip("0") or "1"
        name = f"SYS-GAME-{num}"
        product_range = "SYS"
    elif body.startswith("sysstor"):
        num = body[7:].lstrip("0") or "1"
        name = "SYS-STOR" if num == "1" else f"SYS-STOR-{num}"
        product_range = "SYS"
    elif body.startswith("sys"):
        rest = body[3:].lstrip("0") or "1"
        name = f"SYS-{rest.upper()}"
        product_range = "SYS"
    elif body.startswith("advstor"):
        num = body[7:].lstrip("0") or "1"
        name = f"ADVANCE-STOR-{num}"
        product_range = "ADV"
    elif body.startswith("adv"):
        num = body[3:].lstrip("0") or "1"
        name = f"ADVANCE-{num}"
        product_range = "ADV"
    elif body.startswith("scale"):
        m_sc = re.match(r"^scale(amd|intel|gpu)(\d+)$", body)
        if m_sc:
            arch = {"amd": "a", "intel": "i", "gpu": "gpu-"}[m_sc.group(1)]
            num = int(m_sc.group(2))
            name = f"SCALE-{arch}{num}"
        else:
            name = body.upper()
        product_range = "SCALE"
    elif body.startswith(("hci", "hgr", "hg", "sds")):
        name = f"HGR-{body.upper()}"
        product_range = "HGR"
    elif body.startswith("game"):
        num = body[4:].lstrip("0") or "1"
        name = f"GAME-{num}"
        product_range = "ADV"

    return {
        "planCode": plan_code,
        "baseCode": base_code,
        "name": name,
        "range": product_range,
        "cpu": None,
        "region": region_info,
    }


def classify_product_range(name: str, raw_range: str = "", plan_code: str = "") -> str:
    """Normalize OVH product range into KS / RISE / SYS / ADV / SCALE / HGR."""
    up = (name or "").upper()
    rr = (raw_range or "").lower()
    if up.startswith("KS") or rr == "kimsufi":
        return "KS"
    if up.startswith("RISE") or rr == "rise":
        return "RISE"
    if up.startswith("SYS") or rr == "soyoustart":
        return "SYS"
    if up.startswith(("ADV", "GAME")) or rr in ("advance", "game"):
        return "ADV"
    if up.startswith("SCALE") or rr == "scale":
        return "SCALE"
    if up.startswith(("HGR", "HCI", "SDS")) or rr in ("hg", "high-grade", "sds"):
        return "HGR"
    return infer_plan_from_code(plan_code).get("range", "OTHER")


def parse_memory_code(code: str, catalog_meta: Optional[Dict[str, Any]] = None) -> Dict[str, Any]:
    """
    Parse an OVH memory code like 'ram-32g-noecc-2133' or 'ram-128g-ddr5-on-die-ecc-3600'
    into structured fields:
      { "raw": code, "sizeGB": 32, "ramType": "DDR4", "ecc": "noecc"|"ecc"|"on-die-ecc", "frequency": 2133 }
    """
    if not code or code.lower() in ("none", "n/a"):
        return {"raw": code or "", "sizeGB": 0, "ramType": "", "ecc": "", "frequency": 0}

    raw = code.strip()
    m = re.match(
        r"^ram-(\d+)g(?:-(ddr\d))?(?:-(on-die-ecc|noecc|ecc))?(?:-(\d+))?$",
        raw,
        re.IGNORECASE,
    )
    size_gb = int(m.group(1)) if m else 0
    ram_type = (m.group(2) or "").upper() if m else ""
    ecc_mode = (m.group(3) or "").lower() if m else ""
    freq = int(m.group(4)) if (m and m.group(4)) else 0

    if catalog_meta:
        size_gb = size_gb or int(catalog_meta.get("size") or 0)
        ram_type = ram_type or str(catalog_meta.get("ramType") or "").upper()
        freq = freq or int(catalog_meta.get("frequency") or 0)
        if not ecc_mode and "ecc" in catalog_meta:
            ecc_mode = "ecc" if catalog_meta.get("ecc") else "noecc"

    if not ram_type and freq:
        if freq <= 1866:
            ram_type = "DDR3"
        elif freq <= 3200:
            ram_type = "DDR4"
        else:
            ram_type = "DDR5"

    return {
        "raw": raw,
        "sizeGB": size_gb,
        "ramType": ram_type,
        "ecc": ecc_mode,
        "frequency": freq,
    }


def format_memory(parsed: Dict[str, Any], locale: str = "zh-CN") -> str:
    """Format parsed memory dict into localized friendly text."""
    if not parsed or not parsed.get("sizeGB"):
        return parsed.get("raw", "") if isinstance(parsed, dict) else str(parsed or "")

    unit_gb = "Go" if locale == "fr" else "GB"
    unit_tb = "To" if locale == "fr" else "TB"
    size_str = f"{parsed['sizeGB']} {unit_gb}"
    if parsed["sizeGB"] >= 1024 and parsed["sizeGB"] % 1024 == 0:
        tb = parsed["sizeGB"] // 1024
        size_str = f"{parsed['sizeGB']} {unit_gb} ({tb} {unit_tb})"

    if parsed.get("ramType"):
        size_str = f"{size_str} {parsed['ramType']}"

    parts = [size_str]
    ecc = parsed.get("ecc", "")
    if ecc == "ecc":
        parts.append("ECC")
    elif ecc == "on-die-ecc":
        parts.append("On-Die ECC")
    elif ecc == "noecc":
        parts.append({"zh-CN": "非 ECC", "en": "Non-ECC", "fr": "Sans ECC"}.get(locale, "Non-ECC"))

    if parsed.get("frequency"):
        parts.append(f"{parsed['frequency']} MHz")

    return " · ".join(parts)


def parse_storage_code(code: str, is_system: bool = False) -> Dict[str, Any]:
    """
    Parse an OVH storage or systemStorage code into structured disk groups and RAID type:
      {
        "raw": code,
        "none": bool,
        "raid": "soft" | "hard" | "hybrid_soft" | "none",
        "isSystem": bool,
        "groups": [
          {"count": 2, "capacityGB": 480, "type": "SATA SSD", "pcie": ""}
        ]
      }
    """
    if not code or code.lower() in ("none", "n/a"):
        return {"raw": code or "", "none": True, "raid": "none", "isSystem": is_system, "groups": []}

    raw = code.strip()
    low = raw.lower()
    if low in ("noraid-0", "none"):
        return {"raw": raw, "none": True, "raid": "none", "isSystem": is_system, "groups": []}

    if low.endswith("-system"):
        is_system = True
        low = low[: -len("-system")]

    raid = "none"
    if low.startswith("hardraid-"):
        raid = "hard"
        rest = low[len("hardraid-") :]
    elif low.startswith("hybridsoftraid-"):
        raid = "hybrid_soft"
        rest = low[len("hybridsoftraid-") :]
    elif low.startswith("softraid-"):
        raid = "soft"
        rest = low[len("softraid-") :]
    elif low.startswith("noraid-"):
        raid = "none"
        rest = low[len("noraid-") :]
    else:
        rest = low

    groups: List[Dict[str, Any]] = []
    pattern = re.compile(
        r"(\d+)x(\d+)(?:-(hdd))?-?(sas|sa|sata|ssd|nvme)(?:-(sata|sas))?(?:-(pcie-gen\d))?"
    )
    for m in pattern.finditer(rest):
        count = int(m.group(1))
        cap = int(m.group(2))
        hdd_flag = m.group(3)
        media = m.group(4)
        sub_iface = m.group(5)
        pcie = m.group(6)

        if media == "nvme":
            disk_type = "NVMe SSD"
        elif media == "ssd":
            if sub_iface == "sata":
                disk_type = "SATA SSD"
            elif sub_iface == "sas":
                disk_type = "SAS SSD"
            else:
                disk_type = "SSD"
        elif media in ("sa", "sata"):
            disk_type = "SATA HDD"
        elif media == "sas":
            if hdd_flag == "hdd" or cap >= 10000:
                disk_type = "SAS HDD"
            else:
                disk_type = "SAS SSD"
        else:
            disk_type = media.upper()

        pcie_label = ""
        if pcie:
            pcie_label = pcie.upper().replace("PCIE-GEN", "PCIe Gen")

        groups.append(
            {
                "count": count,
                "capacityGB": cap,
                "type": disk_type,
                "pcie": pcie_label,
            }
        )

    return {
        "raw": raw,
        "none": False,
        "raid": raid,
        "isSystem": is_system,
        "groups": groups,
    }


def format_storage(parsed: Dict[str, Any], locale: str = "zh-CN") -> str:
    """Format parsed storage dict into localized friendly text."""
    if not parsed:
        return ""
    if parsed.get("none"):
        if parsed.get("raw", "").lower() == "noraid-0":
            return {"zh-CN": "无数据盘", "en": "No Data Drive", "fr": "Aucun disque"}[
                locale if locale in ("zh-CN", "en", "fr") else "zh-CN"
            ]
        return ""

    groups = parsed.get("groups") or []
    if not groups:
        return parsed.get("raw", "")

    unit_gb = "Go" if locale == "fr" else "GB"
    unit_tb = "To" if locale == "fr" else "TB"

    group_strs = []
    for g in groups:
        cap_str = format_capacity_gb(g["capacityGB"], unit_gb=unit_gb, unit_tb=unit_tb)
        type_str = g["type"]
        if g.get("pcie"):
            type_str = f"{type_str} ({g['pcie']})"
        group_strs.append(f"{g['count']}× {cap_str} {type_str}")

    joined = " + ".join(group_strs)

    # Per Q2: systemStorage stays concise (e.g. "2× 480 GB SATA SSD"),
    # while main storage appends the RAID type.
    if parsed.get("isSystem"):
        return joined

    raid = parsed.get("raid", "none")
    raid_labels = {
        "soft": {"zh-CN": "软 RAID", "en": "Soft RAID", "fr": "RAID logiciel"},
        "hard": {"zh-CN": "硬 RAID", "en": "Hard RAID", "fr": "RAID matériel"},
        "hybrid_soft": {"zh-CN": "混合软 RAID", "en": "Hybrid Soft RAID", "fr": "RAID hybride"},
    }
    if raid in raid_labels:
        r_label = raid_labels[raid].get(locale, raid_labels[raid]["en"])
        return f"{joined} · {r_label}"
    return joined


def parse_gpu_code(code: str) -> Dict[str, Any]:
    """
    Parse an OVH GPU code like 'gpu-4xnvidia-l40s-48g' or 'gpu-1xradeon-rx6700xt-12g'.
    """
    if not code or code.lower() in ("none", "n/a"):
        return {"raw": code or "", "count": 0, "vendor": "", "model": "", "vramGB": 0}

    raw = code.strip()
    m = re.match(r"^gpu-(\d+)x([a-z0-9-]+?)(?:-(\d+)g)?$", raw, re.IGNORECASE)
    if not m:
        return {"raw": raw, "count": 1, "vendor": "", "model": raw, "vramGB": 0}

    count = int(m.group(1))
    slug = m.group(2).lower()
    vram = int(m.group(3)) if m.group(3) else 0

    vendor = ""
    model = slug.upper()
    if slug.startswith("nvidia-"):
        vendor = "NVIDIA"
        model = slug[len("nvidia-") :].upper()
    elif slug.startswith("radeon-"):
        vendor = "AMD Radeon"
        sub = slug[len("radeon-") :].upper()
        model = re.sub(r"^RX(\d+)(XT)?$", r"RX \1 \2", sub).strip()

    return {
        "raw": raw,
        "count": count,
        "vendor": vendor,
        "model": model,
        "vramGB": vram,
    }


def format_gpu(parsed: Dict[str, Any], locale: str = "zh-CN") -> str:
    """Format parsed GPU dict into friendly string like '4× NVIDIA L40S 48 GB'."""
    if not parsed or not parsed.get("count"):
        return parsed.get("raw", "") if isinstance(parsed, dict) else str(parsed or "")
    unit_gb = "Go" if locale == "fr" else "GB"
    name = f"{parsed['vendor']} {parsed['model']}".strip()
    if parsed.get("vramGB"):
        return f"{parsed['count']}× {name} {parsed['vramGB']} {unit_gb}"
    return f"{parsed['count']}× {name}"


def format_cpu(cpu: Optional[Dict[str, Any]], locale: str = "zh-CN") -> str:
    """Format CPU metadata into localized subtitle string."""
    if not cpu:
        return ""
    brand = cpu.get("brand") or ""
    model = cpu.get("model") or ""
    title = f"{brand} {model}".strip()
    parts = [title] if title else []

    cores = cpu.get("cores")
    threads = cpu.get("threads")
    if cores and threads:
        if locale == "zh-CN":
            parts.append(f"{cores}核/{threads}线程")
        elif locale == "fr":
            parts.append(f"{cores}C/{threads}T")
        else:
            parts.append(f"{cores}C/{threads}T")
    elif cores:
        parts.append(f"{cores}核" if locale == "zh-CN" else f"{cores}C")

    freq = cpu.get("frequency")
    boost = cpu.get("boost")
    if freq and boost and boost != freq:
        parts.append(f"{freq}/{boost} GHz")
    elif freq:
        parts.append(f"{freq} GHz")

    return " · ".join(parts)


class CatalogService:
    """
    Manages the merged Eco + Baremetal public catalog dictionary with a 12h disk cache.
    """

    def __init__(self, cache_dir: str = CACHE_DIR):
        self.cache_dir = cache_dir
        os.makedirs(self.cache_dir, exist_ok=True)
        self._lock = threading.Lock()
        self._memory_cache: Dict[str, Dict[str, Any]] = {}

    def _cache_file(self, endpoint: str, zone: str) -> str:
        safe_ep = re.sub(r"[^a-z0-9_-]", "", (endpoint or "ovh-eu").lower())
        safe_zone = re.sub(r"[^a-z0-9_-]", "", (zone or "IE").upper())
        return os.path.join(self.cache_dir, f"catalog_dict_v2_{safe_ep}_{safe_zone}.json")

    @staticmethod
    def build_dictionary_from_catalogs(
        eco_catalog: Optional[Dict[str, Any]],
        baremetal_catalog: Optional[Dict[str, Any]],
    ) -> Dict[str, Any]:
        """
        Merge raw OVH public Eco + Baremetal catalog JSONs into a compact dictionary:
          {
            "updatedAt": int,
            "plans": { planCode_or_product: { "name": "KS-A", "range": "KS", "cpu": {...} } },
            "options": { optionCode: { ... } }
          }
        """
        products_by_name: Dict[str, Dict[str, Any]] = {}
        addons_price_by_code: Dict[str, Tuple[str, Optional[float]]] = {}
        plans_map: Dict[str, Dict[str, Any]] = {}
        options_map: Dict[str, Dict[str, Any]] = {}
        default_currency = "EUR"

        def _extract_monthly_price(item_obj: Dict[str, Any]) -> Optional[float]:
            pricings = item_obj.get("pricings") or []
            for pr in pricings:
                if pr.get("mode") == "default" and "renew" in (pr.get("capacities") or []):
                    raw_val = pr.get("price")
                    if isinstance(raw_val, (int, float)):
                        return round(raw_val / 1e8, 2)
            for pr in pricings:
                if "renew" in (pr.get("capacities") or []):
                    raw_val = pr.get("price")
                    if isinstance(raw_val, (int, float)):
                        return round(raw_val / 1e8, 2)
            return None

        for cat in (eco_catalog, baremetal_catalog):
            if not isinstance(cat, dict):
                continue
            loc = cat.get("locale") or {}
            if loc.get("currencyCode"):
                default_currency = str(loc["currencyCode"]).upper()
            for addon in cat.get("addons") or []:
                ac = addon.get("planCode")
                if not ac:
                    continue
                prod_code = addon.get("product") or ""
                addons_price_by_code[ac] = (prod_code, _extract_monthly_price(addon))
            for prod in cat.get("products") or []:
                pname = prod.get("name")
                if not pname:
                    continue
                products_by_name[pname] = prod
                tech = (prod.get("blobs") or {}).get("technical") or {}
                if "memory" in tech:
                    options_map[pname] = {
                        "kind": "memory",
                        "parsed": parse_memory_code(pname, tech["memory"]),
                    }
                elif "storage" in tech or pname.startswith(("softraid-", "hardraid-", "hybridsoftraid-", "noraid-")):
                    options_map[pname] = {
                        "kind": "storage",
                        "parsed": parse_storage_code(pname, is_system=pname.endswith("-system")),
                    }
                elif "gpu" in tech or pname.startswith("gpu-"):
                    options_map[pname] = {
                        "kind": "gpu",
                        "parsed": parse_gpu_code(pname),
                    }

        for cat in (eco_catalog, baremetal_catalog):
            if not isinstance(cat, dict):
                continue
            cat_currency = str((cat.get("locale") or {}).get("currencyCode") or default_currency).upper()
            for plan in cat.get("plans") or []:
                plan_code = plan.get("planCode")
                if not plan_code:
                    continue
                product_key = plan.get("product") or ""
                invoice_name = plan.get("invoiceName") or ""
                comm_name = ((plan.get("blobs") or {}).get("commercial") or {}).get("name") or ""

                # Extract short commercial name (e.g. "KS-A | Intel i7-6700k" -> "KS-A")
                parts = [p.strip() for p in invoice_name.split("|") if p.strip()]
                display_name = comm_name or (parts[0] if parts else plan_code)
                invoice_cpu = parts[1] if len(parts) > 1 else ""

                # Extract technical CPU spec from matching product entry
                prod_entry = products_by_name.get(product_key) or products_by_name.get(plan_code) or {}
                srv_tech = ((prod_entry.get("blobs") or {}).get("technical") or {}).get("server") or {}
                cpu_raw = srv_tech.get("cpu") or {}
                raw_range = srv_tech.get("range") or ""

                cpu_info = None
                if cpu_raw.get("model") or invoice_cpu:
                    cpu_info = {
                        "brand": cpu_raw.get("brand") or "",
                        "model": cpu_raw.get("model") or invoice_cpu,
                        "cores": cpu_raw.get("cores"),
                        "threads": cpu_raw.get("threads"),
                        "frequency": cpu_raw.get("frequency"),
                        "boost": cpu_raw.get("boost"),
                    }

                norm_range = classify_product_range(display_name, raw_range, plan_code)
                base_code, region_info = parse_plan_region(plan_code)
                base_price = _extract_monthly_price(plan)

                addon_prices: Dict[str, float] = {}
                for fam in plan.get("addonFamilies") or []:
                    if not isinstance(fam, dict):
                        continue
                    for ac in fam.get("addons") or []:
                        if not isinstance(ac, str):
                            continue
                        prod_code, ap = addons_price_by_code.get(ac, ("", None))
                        if ap is not None and ap > 0:
                            addon_prices[ac] = ap
                            if prod_code:
                                addon_prices[prod_code] = ap

                entry = {
                    "name": display_name,
                    "product": product_key or base_code,
                    "range": norm_range,
                    "cpu": cpu_info,
                    "price": base_price,
                    "currency": cat_currency,
                    "addonPrices": addon_prices,
                }
                plans_map[plan_code] = entry
                is_regional = bool(region_info.get("code"))
                if base_code and (base_code not in plans_map or not is_regional):
                    plans_map[base_code] = entry
                if product_key and (product_key not in plans_map or not is_regional):
                    plans_map[product_key] = entry

        return {
            "updatedAt": int(time.time()),
            "currency": default_currency,
            "plans": plans_map,
            "options": options_map,
        }

    def get_dictionary(
        self,
        endpoint: str = "ovh-eu",
        zone: str = "IE",
        force_refresh: bool = False,
    ) -> Dict[str, Any]:
        """
        Return the merged catalog dictionary, loading from memory/disk cache if fresh
        (<12h) or fetching from OVH public catalog APIs if expired.
        """
        cache_key = f"{endpoint}:{zone}"
        cache_path = self._cache_file(endpoint, zone)
        now = time.time()

        with self._lock:
            mem = self._memory_cache.get(cache_key)
            if mem and not force_refresh and (now - mem.get("updatedAt", 0) < CATALOG_TTL_SECONDS):
                return mem

            if not force_refresh and os.path.exists(cache_path):
                try:
                    with open(cache_path, "r", encoding="utf-8") as f:
                        disk_data = json.load(f)
                    if now - disk_data.get("updatedAt", 0) < CATALOG_TTL_SECONDS:
                        self._memory_cache[cache_key] = disk_data
                        return disk_data
                except Exception:
                    pass

        # Fetch fresh catalogs outside lock
        base_url = ENDPOINT_BASE_URLS.get(endpoint, ENDPOINT_BASE_URLS["ovh-eu"])
        sub = (zone or "IE").upper()
        if endpoint == "ovh-us" and sub not in ("US",):
            sub = "US"
        elif endpoint == "ovh-ca" and sub not in ("CA", "QC", "WE", "WS", "SG", "AU", "IN"):
            sub = "CA"

        eco_data = None
        bm_data = None
        for cat_name in ("eco", "baremetalServers"):
            url = f"{base_url}/order/catalog/public/{cat_name}?ovhSubsidiary={sub}"
            try:
                if _requests is not None:
                    resp = _requests.get(url, timeout=25)
                    if resp.status_code == 200:
                        data = resp.json()
                    else:
                        data = None
                else:
                    req = urllib.request.Request(url, headers={"Accept": "application/json"})
                    with urllib.request.urlopen(req, timeout=25) as r:
                        data = json.loads(r.read().decode("utf-8"))
                if data:
                    if cat_name == "eco":
                        eco_data = data
                    else:
                        bm_data = data
            except Exception:
                pass

        with self._lock:
            if eco_data or bm_data:
                merged = self.build_dictionary_from_catalogs(eco_data, bm_data)
                try:
                    with open(cache_path, "w", encoding="utf-8") as f:
                        json.dump(merged, f, ensure_ascii=False)
                except Exception:
                    pass
                self._memory_cache[cache_key] = merged
                return merged

            # Fallback to stale disk cache if network failed
            if os.path.exists(cache_path):
                try:
                    with open(cache_path, "r", encoding="utf-8") as f:
                        stale = json.load(f)
                    self._memory_cache[cache_key] = stale
                    return stale
                except Exception:
                    pass

            empty = {"updatedAt": int(now), "plans": {}, "options": {}}
            self._memory_cache[cache_key] = empty
            return empty

    def resolve_plan(
        self,
        plan_code: str,
        server_code: str = "",
        endpoint: str = "ovh-eu",
        zone: str = "IE",
        dictionary: Optional[Dict[str, Any]] = None,
    ) -> Dict[str, Any]:
        """
        Resolve a planCode (and optional server code) to its friendly display metadata:
          {
            "planCode": "24ska01-v1",
            "baseCode": "24ska01",
            "name": "KS-A",
            "range": "KS",
            "cpu": {...},
            "cpuText": "Intel i7-6700k · 4核/8线程 · 4/4.2 GHz",
            "region": {"code": "SGP", "zh-CN": "新加坡", ...} | None
          }
        """
        d = dictionary if dictionary is not None else self.get_dictionary(endpoint=endpoint, zone=zone)
        plans = (d or {}).get("plans") or {}
        base_code, region_info = parse_plan_region(plan_code)
        clean_base = re.sub(r"^(\d{2}[a-z]+\d{2})\d$", r"\1", base_code.lower())

        match = (
            plans.get(plan_code)
            or (plans.get(server_code) if server_code else None)
            or plans.get(base_code)
            or plans.get(clean_base)
        )
        if match:
            return {
                "planCode": plan_code,
                "baseCode": base_code or plan_code,
                "name": match.get("name") or plan_code,
                "range": match.get("range") or classify_product_range(match.get("name", ""), "", plan_code),
                "cpu": match.get("cpu"),
                "region": region_info,
                "price": match.get("price"),
                "currency": match.get("currency") or (d or {}).get("currency") or "EUR",
                "addonPrices": match.get("addonPrices") or {},
            }

        return infer_plan_from_code(plan_code)

    def format_hardware_spec(
        self,
        memory_code: str = "",
        storage_code: str = "",
        system_storage_code: str = "",
        gpu_code: str = "",
        locale: str = "zh-CN",
        dictionary: Optional[Dict[str, Any]] = None,
    ) -> Dict[str, str]:
        """
        Return localized friendly strings for memory, storage, systemStorage, and gpu.
        """
        opts = ((dictionary or {}).get("options")) or {}

        mem_parsed = (opts.get(memory_code) or {}).get("parsed") or parse_memory_code(memory_code)
        st_parsed = (opts.get(storage_code) or {}).get("parsed") or parse_storage_code(storage_code, is_system=False)
        sys_parsed = (opts.get(system_storage_code) or {}).get("parsed") or parse_storage_code(
            system_storage_code, is_system=True
        )
        gpu_parsed = (opts.get(gpu_code) or {}).get("parsed") or parse_gpu_code(gpu_code)

        return {
            "memory": format_memory(mem_parsed, locale=locale) if memory_code else "",
            "storage": format_storage(st_parsed, locale=locale) if storage_code else "",
            "systemStorage": format_storage(sys_parsed, locale=locale) if system_storage_code else "",
            "gpu": format_gpu(gpu_parsed, locale=locale) if gpu_code else "",
        }


# Singleton instance
catalog_service = CatalogService()
