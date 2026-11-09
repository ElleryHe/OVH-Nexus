import { useEffect, useState } from "react";
import { api } from "@/utils/apiClient";
import type { SupportedLocale } from "@/context/LocaleContext";

export interface CpuInfo {
  brand?: string;
  model?: string;
  cores?: number;
  threads?: number;
  frequency?: number;
  boost?: number;
}

export interface RegionTagInfo {
  code: string;
  "zh-CN": string;
  en: string;
  fr: string;
}

export interface PlanDisplayInfo {
  planCode: string;
  baseCode: string;
  name: string;
  range: string;
  cpu: CpuInfo | null;
  cpuText: string;
  region: RegionTagInfo | null;
  regionLabel: string;
  basePrice?: number | null;
  currency?: string;
  addonPrices?: Record<string, number>;
}

export interface CatalogDictionary {
  updatedAt: number;
  currency?: string;
  plans: Record<
    string,
    {
      name: string;
      product?: string;
      range?: string;
      cpu?: CpuInfo | null;
      price?: number | null;
      currency?: string;
      addonPrices?: Record<string, number>;
    }
  >;
  options: Record<string, any>;
}

export interface ConfigPriceBreakdown {
  totalPrice: number | null;
  basePrice: number | null;
  memoryDelta: number;
  storageDelta: number;
  systemStorageDelta: number;
  gpuDelta: number;
  currency: string;
  symbol: string;
  formattedTotal: string;
}

const STORAGE_KEY = "ovh_nexus_catalog_dict_v2";
let memoryDict: CatalogDictionary | null = null;
let inflightPromise: Promise<CatalogDictionary | null> | null = null;

const REGION_SUFFIX_MAP: Record<string, RegionTagInfo> = {
  sgp: { code: "SGP", "zh-CN": "新加坡", en: "Singapore", fr: "Singapour" },
  syd: { code: "SYD", "zh-CN": "悉尼", en: "Sydney", fr: "Sydney" },
  mum: { code: "MUM", "zh-CN": "孟买", en: "Mumbai", fr: "Mumbai" },
  apac: { code: "APAC", "zh-CN": "亚太", en: "APAC", fr: "APAC" },
};

export function formatCapacityGB(
  capGB: number,
  locale: SupportedLocale = "zh-CN"
): string {
  const unitGB = locale === "fr" ? "Go" : "GB";
  const unitTB = locale === "fr" ? "To" : "TB";
  if (!capGB || capGB <= 0) return `0 ${unitGB}`;
  if (capGB >= 1000) {
    if (capGB % 1000 === 0) {
      return `${capGB / 1000} ${unitTB}`;
    }
    const tb = (capGB / 1000).toFixed(2).replace(/\.?0+$/, "");
    return `${tb} ${unitTB}`;
  }
  return `${capGB} ${unitGB}`;
}

export function parsePlanRegion(planCode: string): {
  baseCode: string;
  region: RegionTagInfo | null;
} {
  if (!planCode) return { baseCode: "", region: null };
  let code = planCode.trim();
  let region: RegionTagInfo | null = null;
  const regMatch = code.match(/-(sgp|syd|mum|apac)$/i);
  if (regMatch) {
    const key = regMatch[1].toLowerCase();
    region = REGION_SUFFIX_MAP[key] || null;
    code = code.slice(0, regMatch.index);
  }
  const baseCode = code.replace(/-v\d+$/i, "");
  return { baseCode, region };
}

export function formatCpu(
  cpu: CpuInfo | null | undefined,
  locale: SupportedLocale = "zh-CN"
): string {
  if (!cpu) return "";
  const title = [cpu.brand, cpu.model].filter(Boolean).join(" ").trim();
  const parts: string[] = [];
  if (title) parts.push(title);

  if (cpu.cores && cpu.threads) {
    parts.push(
      locale === "zh-CN"
        ? `${cpu.cores}核/${cpu.threads}线程`
        : `${cpu.cores}C/${cpu.threads}T`
    );
  } else if (cpu.cores) {
    parts.push(locale === "zh-CN" ? `${cpu.cores}核` : `${cpu.cores}C`);
  }

  if (cpu.frequency && cpu.boost && cpu.boost !== cpu.frequency) {
    parts.push(`${cpu.frequency}/${cpu.boost} GHz`);
  } else if (cpu.frequency) {
    parts.push(`${cpu.frequency} GHz`);
  }

  return parts.join(" · ");
}

export function inferPlanFromCode(
  planCode: string,
  locale: SupportedLocale = "zh-CN"
): PlanDisplayInfo {
  const { baseCode, region } = parsePlanRegion(planCode);
  const clean = baseCode
    .toLowerCase()
    .replace(/^(\d{2}[a-z]+\d{2})\d$/, "$1");
  const m = clean.match(/^(\d{2})([a-z0-9-]+)$/);
  const body = m ? m[2] : clean;

  let name = planCode.toUpperCase();
  let range = "OTHER";

  if (body.startsWith("ska")) {
    const idx = body.slice(3).replace(/^0+/, "") || "A";
    name = body === "ska01" ? "KS-A" : `KS-A${idx}`;
    range = "KS";
  } else if (body.startsWith("skb")) {
    const idx = body.slice(3).replace(/^0+/, "");
    name = body === "skb01" ? "KS-B" : `KS-B${idx}`;
    range = "KS";
  } else if (body.startsWith("skc")) {
    const idx = body.slice(3).replace(/^0+/, "");
    name = body === "skc01" ? "KS-C" : `KS-C${idx}`;
    range = "KS";
  } else if (body.startsWith("skle")) {
    const num = body.slice(4).replace(/^0+/, "") || "1";
    name = `KS-LE-${num}`;
    range = "KS";
  } else if (body.startsWith("skgame")) {
    const num = body.slice(6).replace(/^0+/, "") || "1";
    name = `KS-GAME-${num}`;
    range = "KS";
  } else if (body.startsWith("skstor")) {
    const num = body.slice(6).replace(/^0+/, "") || "1";
    name = `KS-STOR-${num}`;
    range = "KS";
  } else if (body.startsWith("sk")) {
    const rest = body.slice(2);
    const nm = rest.match(/^(\d+)([a-z]*)$/);
    if (nm) {
      let n = parseInt(nm[1], 10);
      const suffix = nm[2].toUpperCase();
      if (n >= 10 && n % 10 === 0) n = n / 10;
      name = `KS-${n}${suffix ? "-" + suffix : ""}`;
    }
    range = "KS";
  } else if (body.startsWith("risele")) {
    const num = body.slice(6).replace(/^0+/, "") || "1";
    name = `RISE-LE-${num}`;
    range = "RISE";
  } else if (body.startsWith("risegame")) {
    const num = body.slice(8).replace(/^0+/, "") || "1";
    name = `RISE-GAME-${num}`;
    range = "RISE";
  } else if (body.startsWith("risestor")) {
    const num = body.slice(8).replace(/^0+/, "") || "1";
    name = num === "1" ? "RISE-STOR" : `RISE-STOR-${num}`;
    range = "RISE";
  } else if (body.startsWith("rise")) {
    const rest = body.slice(4);
    const sz = rest.match(/^(s|m|l|xl)(\d+)$/);
    if (sz) {
      name = `RISE-${sz[1].toUpperCase()}`;
    } else {
      const num = rest.replace(/^0+/, "") || "1";
      name = `RISE-${num}`;
    }
    range = "RISE";
  } else if (body.startsWith("sysstorle")) {
    const num = body.slice(9).replace(/^0+/, "") || "1";
    name = `SYS-STOR-LE-${num}`;
    range = "SYS";
  } else if (body.startsWith("sysle")) {
    const num = body.slice(5).replace(/^0+/, "") || "1";
    name = `SYS-LE-${num}`;
    range = "SYS";
  } else if (body.startsWith("sysgame")) {
    const num = body.slice(7).replace(/^0+/, "") || "1";
    name = `SYS-GAME-${num}`;
    range = "SYS";
  } else if (body.startsWith("sysstor")) {
    const num = body.slice(7).replace(/^0+/, "") || "1";
    name = num === "1" ? "SYS-STOR" : `SYS-STOR-${num}`;
    range = "SYS";
  } else if (body.startsWith("sys")) {
    const rest = body.slice(3).replace(/^0+/, "") || "1";
    name = `SYS-${rest.toUpperCase()}`;
    range = "SYS";
  } else if (body.startsWith("advstor")) {
    const num = body.slice(7).replace(/^0+/, "") || "1";
    name = `ADVANCE-STOR-${num}`;
    range = "ADV";
  } else if (body.startsWith("adv")) {
    const num = body.slice(3).replace(/^0+/, "") || "1";
    name = `ADVANCE-${num}`;
    range = "ADV";
  } else if (body.startsWith("scale")) {
    const sc = body.match(/^scale(amd|intel|gpu)(\d+)$/);
    if (sc) {
      const map: Record<string, string> = { amd: "a", intel: "i", gpu: "gpu-" };
      name = `SCALE-${map[sc[1]] || ""}${parseInt(sc[2], 10)}`;
    } else {
      name = body.toUpperCase();
    }
    range = "SCALE";
  } else if (
    body.startsWith("hci") ||
    body.startsWith("hgr") ||
    body.startsWith("hg") ||
    body.startsWith("sds")
  ) {
    name = `HGR-${body.toUpperCase()}`;
    range = "HGR";
  } else if (body.startsWith("game")) {
    const num = body.slice(4).replace(/^0+/, "") || "1";
    name = `GAME-${num}`;
    range = "ADV";
  }

  return {
    planCode,
    baseCode: baseCode || planCode,
    name,
    range,
    cpu: null,
    cpuText: "",
    region,
    regionLabel: region ? region[locale] || region.en : "",
  };
}

export function resolvePlanDisplay(
  planCode: string,
  serverCode: string = "",
  locale: SupportedLocale = "zh-CN",
  dict?: CatalogDictionary | null
): PlanDisplayInfo {
  if (!planCode) {
    return {
      planCode: "",
      baseCode: "",
      name: "",
      range: "OTHER",
      cpu: null,
      cpuText: "",
      region: null,
      regionLabel: "",
    };
  }
  const activeDict = dict ?? memoryDict;
  const { baseCode, region } = parsePlanRegion(planCode);
  const cleanBase = baseCode
    .toLowerCase()
    .replace(/^(\d{2}[a-z]+\d{2})\d$/, "$1");

  const plans = activeDict?.plans || {};
  const match =
    plans[planCode] ||
    (serverCode ? plans[serverCode] : undefined) ||
    plans[baseCode] ||
    plans[cleanBase];

  if (match) {
    const cpu = match.cpu || null;
    return {
      planCode,
      baseCode: baseCode || planCode,
      name: match.name || planCode,
      range: match.range || "OTHER",
      cpu,
      cpuText: formatCpu(cpu, locale),
      region,
      regionLabel: region ? region[locale] || region.en : "",
      basePrice: typeof match.price === "number" ? match.price : null,
      currency: match.currency || activeDict?.currency || "EUR",
      addonPrices: match.addonPrices || {},
    };
  }

  return inferPlanFromCode(planCode, locale);
}

const CURRENCY_SYMBOL_MAP: Record<string, string> = {
  EUR: "€",
  USD: "$",
  GBP: "£",
  CAD: "CA$",
  AUD: "A$",
  SGD: "S$",
  INR: "₹",
};

export function resolveConfigPrice(
  planMeta: PlanDisplayInfo,
  options?: {
    memory?: string;
    storage?: string;
    systemStorage?: string;
    gpu?: string;
  }
): ConfigPriceBreakdown {
  const currency = (planMeta.currency || "EUR").toUpperCase();
  const symbol = CURRENCY_SYMBOL_MAP[currency] || `${currency} `;
  const basePrice =
    typeof planMeta.basePrice === "number" ? planMeta.basePrice : null;
  if (basePrice === null) {
    return {
      totalPrice: null,
      basePrice: null,
      memoryDelta: 0,
      storageDelta: 0,
      systemStorageDelta: 0,
      gpuDelta: 0,
      currency,
      symbol,
      formattedTotal: "",
    };
  }
  const addonMap = planMeta.addonPrices || {};
  const lookupAddon = (code?: string): number => {
    if (!code || code === "none" || code === "N/A") return 0;
    if (typeof addonMap[code] === "number") return addonMap[code];
    for (const [k, v] of Object.entries(addonMap)) {
      if (k.startsWith(`${code}-`) && typeof v === "number") {
        return v;
      }
    }
    return 0;
  };
  const memoryDelta = lookupAddon(options?.memory);
  const storageDelta = lookupAddon(options?.storage);
  const systemStorageDelta = lookupAddon(options?.systemStorage);
  const gpuDelta = lookupAddon(options?.gpu);
  const totalPrice = Number(
    (
      basePrice +
      memoryDelta +
      storageDelta +
      systemStorageDelta +
      gpuDelta
    ).toFixed(2)
  );
  const formattedTotal = `${symbol}${totalPrice.toLocaleString("en-US", {
    minimumFractionDigits: 2,
    maximumFractionDigits: 2,
  })}`;
  return {
    totalPrice,
    basePrice,
    memoryDelta,
    storageDelta,
    systemStorageDelta,
    gpuDelta,
    currency,
    symbol,
    formattedTotal,
  };
}

export function formatMemoryCode(
  code: string,
  locale: SupportedLocale = "zh-CN"
): string {
  if (!code || code.toLowerCase() === "none" || code.toLowerCase() === "n/a") {
    return "";
  }
  const raw = code.trim();
  const m = raw.match(
    /^ram-(\d+)g(?:-(ddr\d))?(?:-(on-die-ecc|noecc|ecc))?(?:-(\d+))?$/i
  );
  if (!m) return raw;

  const sizeGB = parseInt(m[1], 10);
  let ramType = (m[2] || "").toUpperCase();
  const eccMode = (m[3] || "").toLowerCase();
  const freq = m[4] ? parseInt(m[4], 10) : 0;

  if (!ramType && freq) {
    if (freq <= 1866) ramType = "DDR3";
    else if (freq <= 3200) ramType = "DDR4";
    else ramType = "DDR5";
  }

  const unitGB = locale === "fr" ? "Go" : "GB";
  const unitTB = locale === "fr" ? "To" : "TB";
  let sizeStr = `${sizeGB} ${unitGB}`;
  if (sizeGB >= 1024 && sizeGB % 1024 === 0) {
    sizeStr = `${sizeGB} ${unitGB} (${sizeGB / 1024} ${unitTB})`;
  }
  if (ramType) {
    sizeStr = `${sizeStr} ${ramType}`;
  }

  const parts = [sizeStr];
  if (eccMode === "ecc") {
    parts.push("ECC");
  } else if (eccMode === "on-die-ecc") {
    parts.push("On-Die ECC");
  } else if (eccMode === "noecc") {
    parts.push(
      locale === "zh-CN" ? "非 ECC" : locale === "fr" ? "Sans ECC" : "Non-ECC"
    );
  }
  if (freq) {
    parts.push(`${freq} MHz`);
  }
  return parts.join(" · ");
}

export function formatStorageCode(
  code: string,
  locale: SupportedLocale = "zh-CN",
  isSystem: boolean = false
): string {
  if (!code || code.toLowerCase() === "none" || code.toLowerCase() === "n/a") {
    return "";
  }
  const raw = code.trim();
  let low = raw.toLowerCase();
  if (low === "noraid-0") {
    return locale === "zh-CN"
      ? "无数据盘"
      : locale === "fr"
      ? "Aucun disque"
      : "No Data Drive";
  }
  if (low.endsWith("-system")) {
    isSystem = true;
    low = low.slice(0, -"-system".length);
  }

  let raid: "soft" | "hard" | "hybrid_soft" | "none" = "none";
  let rest = low;
  if (low.startsWith("hardraid-")) {
    raid = "hard";
    rest = low.slice("hardraid-".length);
  } else if (low.startsWith("hybridsoftraid-")) {
    raid = "hybrid_soft";
    rest = low.slice("hybridsoftraid-".length);
  } else if (low.startsWith("softraid-")) {
    raid = "soft";
    rest = low.slice("softraid-".length);
  } else if (low.startsWith("noraid-")) {
    raid = "none";
    rest = low.slice("noraid-".length);
  }

  const regex =
    /(\d+)x(\d+)(?:-(hdd))?-?(sas|sa|sata|ssd|nvme)(?:-(sata|sas))?(?:-(pcie-gen\d))?/g;
  const groupStrs: string[] = [];
  let match: RegExpExecArray | null;

  while ((match = regex.exec(rest)) !== null) {
    const count = parseInt(match[1], 10);
    const cap = parseInt(match[2], 10);
    const hddFlag = match[3];
    const media = match[4];
    const subIface = match[5];
    const pcie = match[6];

    let diskType = media.toUpperCase();
    if (media === "nvme") {
      diskType = "NVMe SSD";
    } else if (media === "ssd") {
      diskType =
        subIface === "sata"
          ? "SATA SSD"
          : subIface === "sas"
          ? "SAS SSD"
          : "SSD";
    } else if (media === "sa" || media === "sata") {
      diskType = "SATA HDD";
    } else if (media === "sas") {
      diskType = hddFlag === "hdd" || cap >= 10000 ? "SAS HDD" : "SAS SSD";
    }

    if (pcie) {
      const pcieLabel = pcie.toUpperCase().replace("PCIE-GEN", "PCIe Gen");
      diskType = `${diskType} (${pcieLabel})`;
    }

    const capStr = formatCapacityGB(cap, locale);
    groupStrs.push(`${count}× ${capStr} ${diskType}`);
  }

  if (groupStrs.length === 0) return raw;
  const joined = groupStrs.join(" + ");
  if (isSystem) return joined;

  const raidLabels: Record<string, Record<SupportedLocale, string>> = {
    soft: { "zh-CN": "软 RAID", en: "Soft RAID", fr: "RAID logiciel" },
    hard: { "zh-CN": "硬 RAID", en: "Hard RAID", fr: "RAID matériel" },
    hybrid_soft: {
      "zh-CN": "混合软 RAID",
      en: "Hybrid Soft RAID",
      fr: "RAID hybride",
    },
  };
  if (raid in raidLabels) {
    return `${joined} · ${raidLabels[raid][locale] || raidLabels[raid].en}`;
  }
  return joined;
}

export function formatGpuCode(
  code: string,
  locale: SupportedLocale = "zh-CN"
): string {
  if (!code || code.toLowerCase() === "none" || code.toLowerCase() === "n/a") {
    return "";
  }
  const raw = code.trim();
  const m = raw.match(/^gpu-(\d+)x([a-z0-9-]+?)(?:-(\d+)g)?$/i);
  if (!m) return raw;

  const count = parseInt(m[1], 10);
  const slug = m[2].toLowerCase();
  const vram = m[3] ? parseInt(m[3], 10) : 0;
  const unitGB = locale === "fr" ? "Go" : "GB";

  let name = slug.toUpperCase();
  if (slug.startsWith("nvidia-")) {
    name = `NVIDIA ${slug.slice("nvidia-".length).toUpperCase()}`;
  } else if (slug.startsWith("radeon-")) {
    const sub = slug
      .slice("radeon-".length)
      .toUpperCase()
      .replace(/^RX(\d+)(XT)?$/, "RX $1 $2")
      .trim();
    name = `AMD Radeon ${sub}`;
  }

  return vram ? `${count}× ${name} ${vram} ${unitGB}` : `${count}× ${name}`;
}

export function formatOptionCode(
  code: string,
  locale: SupportedLocale = "zh-CN"
): string {
  if (!code) return "";
  const low = code.trim().toLowerCase();
  if (low.startsWith("ram-")) return formatMemoryCode(code, locale);
  if (low.endsWith("-system")) return formatStorageCode(code, locale, true);
  if (
    low.startsWith("softraid-") ||
    low.startsWith("hardraid-") ||
    low.startsWith("hybridsoftraid-") ||
    low.startsWith("noraid-")
  ) {
    return formatStorageCode(code, locale, false);
  }
  if (low.startsWith("gpu-")) return formatGpuCode(code, locale);
  return code;
}

export async function loadCatalogDictionary(
  forceRefresh: boolean = false
): Promise<CatalogDictionary | null> {
  if (!forceRefresh && memoryDict) {
    return memoryDict;
  }
  if (!forceRefresh) {
    try {
      const cached = localStorage.getItem(STORAGE_KEY);
      if (cached) {
        const parsed = JSON.parse(cached) as CatalogDictionary;
        if (
          parsed &&
          parsed.plans &&
          Date.now() / 1000 - (parsed.updatedAt || 0) < 12 * 3600
        ) {
          memoryDict = parsed;
        }
      }
    } catch {
      // ignore storage errors
    }
  }
  if (inflightPromise && !forceRefresh) {
    return inflightPromise;
  }
  inflightPromise = api
    .get("/catalog/dictionary", {
      params: forceRefresh ? { refresh: "1" } : undefined,
    })
    .then((res) => {
      if (res.data && res.data.plans) {
        memoryDict = res.data as CatalogDictionary;
        try {
          localStorage.setItem(STORAGE_KEY, JSON.stringify(memoryDict));
        } catch {
          // ignore quota error
        }
      }
      return memoryDict;
    })
    .catch(() => memoryDict)
    .finally(() => {
      inflightPromise = null;
    });
  return inflightPromise;
}

export function useCatalogDictionary() {
  const [dict, setDict] = useState<CatalogDictionary | null>(() => {
    if (memoryDict) return memoryDict;
    try {
      const raw = localStorage.getItem(STORAGE_KEY);
      if (raw) {
        memoryDict = JSON.parse(raw);
        return memoryDict;
      }
    } catch {
      // ignore
    }
    return null;
  });

  useEffect(() => {
    let mounted = true;
    loadCatalogDictionary().then((d) => {
      if (mounted && d) setDict(d);
    });
    return () => {
      mounted = false;
    };
  }, []);

  return dict;
}
