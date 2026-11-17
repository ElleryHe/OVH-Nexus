import type { Locale } from '@/context/LocaleContext';

export type DatacenterRegion = 'Europe' | 'North America' | 'Asia Pacific';

export interface LocalizedText {
  en: string;
  fr: string;
  'zh-CN': string;
}

export interface DatacenterMeta {
  /** Canonical lowercase code (e.g. 'bhs', 'tor', 'gra', 'mum') */
  id: string;
  /** Canonical uppercase display code (e.g. 'BHS', 'TOR', 'GRA', 'MUM') */
  code: string;
  /** OVH API datacenter code (e.g. 'ynm' for Mumbai, 'ca-east-tor' or 'tor') */
  apiCode: string;
  /** Country flag emoji */
  flag: string;
  /** 2-letter ISO country code in lowercase */
  countryCode: string;
  /** Tri-lingual city name */
  city: LocalizedText;
  /** Tri-lingual country name */
  country: LocalizedText;
  /** Macro geographic region */
  region: DatacenterRegion;
  /** Whether this is a primary bare-metal / cloud datacenter */
  primary?: boolean;
}

export const DATACENTER_DICTIONARY: Record<string, DatacenterMeta> = {
  bhs: {
    id: 'bhs',
    code: 'BHS',
    apiCode: 'bhs',
    flag: '🇨🇦',
    countryCode: 'ca',
    city: {
      en: 'Beauharnois',
      fr: 'Beauharnois',
      'zh-CN': '博阿努瓦',
    },
    country: {
      en: 'Canada',
      fr: 'Canada',
      'zh-CN': '加拿大',
    },
    region: 'North America',
    primary: true,
  },
  tor: {
    id: 'tor',
    code: 'TOR',
    apiCode: 'tor',
    flag: '🇨🇦',
    countryCode: 'ca',
    city: {
      en: 'Toronto',
      fr: 'Toronto',
      'zh-CN': '多伦多',
    },
    country: {
      en: 'Canada',
      fr: 'Canada',
      'zh-CN': '加拿大',
    },
    region: 'North America',
    primary: true,
  },
  gra: {
    id: 'gra',
    code: 'GRA',
    apiCode: 'gra',
    flag: '🇫🇷',
    countryCode: 'fr',
    city: {
      en: 'Gravelines',
      fr: 'Gravelines',
      'zh-CN': '格拉沃利讷',
    },
    country: {
      en: 'France',
      fr: 'France',
      'zh-CN': '法国',
    },
    region: 'Europe',
    primary: true,
  },
  rbx: {
    id: 'rbx',
    code: 'RBX',
    apiCode: 'rbx',
    flag: '🇫🇷',
    countryCode: 'fr',
    city: {
      en: 'Roubaix',
      fr: 'Roubaix',
      'zh-CN': '鲁贝',
    },
    country: {
      en: 'France',
      fr: 'France',
      'zh-CN': '法国',
    },
    region: 'Europe',
    primary: true,
  },
  sbg: {
    id: 'sbg',
    code: 'SBG',
    apiCode: 'sbg',
    flag: '🇫🇷',
    countryCode: 'fr',
    city: {
      en: 'Strasbourg',
      fr: 'Strasbourg',
      'zh-CN': '斯特拉斯堡',
    },
    country: {
      en: 'France',
      fr: 'France',
      'zh-CN': '法国',
    },
    region: 'Europe',
    primary: true,
  },
  par: {
    id: 'par',
    code: 'PAR',
    apiCode: 'par',
    flag: '🇫🇷',
    countryCode: 'fr',
    city: {
      en: 'Paris',
      fr: 'Paris',
      'zh-CN': '巴黎',
    },
    country: {
      en: 'France',
      fr: 'France',
      'zh-CN': '法国',
    },
    region: 'Europe',
  },
  mrs: {
    id: 'mrs',
    code: 'MRS',
    apiCode: 'mrs',
    flag: '🇫🇷',
    countryCode: 'fr',
    city: {
      en: 'Marseille',
      fr: 'Marseille',
      'zh-CN': '马赛',
    },
    country: {
      en: 'France',
      fr: 'France',
      'zh-CN': '法国',
    },
    region: 'Europe',
  },
  lim: {
    id: 'lim',
    code: 'LIM',
    apiCode: 'lim',
    flag: '🇩🇪',
    countryCode: 'de',
    city: {
      en: 'Limburg',
      fr: 'Limbourg',
      'zh-CN': '林堡',
    },
    country: {
      en: 'Germany',
      fr: 'Allemagne',
      'zh-CN': '德国',
    },
    region: 'Europe',
    primary: true,
  },
  fra: {
    id: 'fra',
    code: 'FRA',
    apiCode: 'fra',
    flag: '🇩🇪',
    countryCode: 'de',
    city: {
      en: 'Frankfurt',
      fr: 'Francfort',
      'zh-CN': '法兰克福',
    },
    country: {
      en: 'Germany',
      fr: 'Allemagne',
      'zh-CN': '德国',
    },
    region: 'Europe',
    primary: true,
  },
  eri: {
    id: 'eri',
    code: 'ERI',
    apiCode: 'eri',
    flag: '🇬🇧',
    countryCode: 'gb',
    city: {
      en: 'Erith (London)',
      fr: 'Erith (Londres)',
      'zh-CN': '埃里斯（伦敦）',
    },
    country: {
      en: 'United Kingdom',
      fr: 'Royaume-Uni',
      'zh-CN': '英国',
    },
    region: 'Europe',
    primary: true,
  },
  lon: {
    id: 'lon',
    code: 'LON',
    apiCode: 'lon',
    flag: '🇬🇧',
    countryCode: 'gb',
    city: {
      en: 'London',
      fr: 'Londres',
      'zh-CN': '伦敦',
    },
    country: {
      en: 'United Kingdom',
      fr: 'Royaume-Uni',
      'zh-CN': '英国',
    },
    region: 'Europe',
    primary: true,
  },
  waw: {
    id: 'waw',
    code: 'WAW',
    apiCode: 'waw',
    flag: '🇵🇱',
    countryCode: 'pl',
    city: {
      en: 'Warsaw',
      fr: 'Varsovie',
      'zh-CN': '华沙',
    },
    country: {
      en: 'Poland',
      fr: 'Pologne',
      'zh-CN': '波兰',
    },
    region: 'Europe',
    primary: true,
  },
  vin: {
    id: 'vin',
    code: 'VIN',
    apiCode: 'vin',
    flag: '🇺🇸',
    countryCode: 'us',
    city: {
      en: 'Vint Hill (VA)',
      fr: 'Vint Hill (Virginie)',
      'zh-CN': '温特山（弗吉尼亚）',
    },
    country: {
      en: 'United States',
      fr: 'États-Unis',
      'zh-CN': '美国',
    },
    region: 'North America',
    primary: true,
  },
  hil: {
    id: 'hil',
    code: 'HIL',
    apiCode: 'hil',
    flag: '🇺🇸',
    countryCode: 'us',
    city: {
      en: 'Hillsboro (OR)',
      fr: 'Hillsboro (Oregon)',
      'zh-CN': '希尔斯伯勒（俄勒冈）',
    },
    country: {
      en: 'United States',
      fr: 'États-Unis',
      'zh-CN': '美国',
    },
    region: 'North America',
    primary: true,
  },
  syd: {
    id: 'syd',
    code: 'SYD',
    apiCode: 'syd',
    flag: '🇦🇺',
    countryCode: 'au',
    city: {
      en: 'Sydney',
      fr: 'Sydney',
      'zh-CN': '悉尼',
    },
    country: {
      en: 'Australia',
      fr: 'Australie',
      'zh-CN': '澳大利亚',
    },
    region: 'Asia Pacific',
    primary: true,
  },
  sgp: {
    id: 'sgp',
    code: 'SGP',
    apiCode: 'sgp',
    flag: '🇸🇬',
    countryCode: 'sg',
    city: {
      en: 'Singapore',
      fr: 'Singapour',
      'zh-CN': '新加坡',
    },
    country: {
      en: 'Singapore',
      fr: 'Singapour',
      'zh-CN': '新加坡',
    },
    region: 'Asia Pacific',
    primary: true,
  },
  mum: {
    id: 'mum',
    code: 'MUM',
    apiCode: 'ynm',
    flag: '🇮🇳',
    countryCode: 'in',
    city: {
      en: 'Mumbai',
      fr: 'Bombay',
      'zh-CN': '孟买',
    },
    country: {
      en: 'India',
      fr: 'Inde',
      'zh-CN': '印度',
    },
    region: 'Asia Pacific',
    primary: true,
  },
  bru: {
    id: 'bru',
    code: 'BRU',
    apiCode: 'bru',
    flag: '🇧🇪',
    countryCode: 'be',
    city: {
      en: 'Brussels',
      fr: 'Bruxelles',
      'zh-CN': '布鲁塞尔',
    },
    country: {
      en: 'Belgium',
      fr: 'Belgique',
      'zh-CN': '比利时',
    },
    region: 'Europe',
  },
  mad: {
    id: 'mad',
    code: 'MAD',
    apiCode: 'mad',
    flag: '🇪🇸',
    countryCode: 'es',
    city: {
      en: 'Madrid',
      fr: 'Madrid',
      'zh-CN': '马德里',
    },
    country: {
      en: 'Spain',
      fr: 'Espagne',
      'zh-CN': '西班牙',
    },
    region: 'Europe',
  },
  mil: {
    id: 'mil',
    code: 'MIL',
    apiCode: 'mil',
    flag: '🇮🇹',
    countryCode: 'it',
    city: {
      en: 'Milan',
      fr: 'Milan',
      'zh-CN': '米兰',
    },
    country: {
      en: 'Italy',
      fr: 'Italie',
      'zh-CN': '意大利',
    },
    region: 'Europe',
  },
  zur: {
    id: 'zur',
    code: 'ZUR',
    apiCode: 'zur',
    flag: '🇨🇭',
    countryCode: 'ch',
    city: {
      en: 'Zurich',
      fr: 'Zurich',
      'zh-CN': '苏黎世',
    },
    country: {
      en: 'Switzerland',
      fr: 'Suisse',
      'zh-CN': '瑞士',
    },
    region: 'Europe',
  },
  ams: {
    id: 'ams',
    code: 'AMS',
    apiCode: 'ams',
    flag: '🇳🇱',
    countryCode: 'nl',
    city: {
      en: 'Amsterdam',
      fr: 'Amsterdam',
      'zh-CN': '阿姆斯特丹',
    },
    country: {
      en: 'Netherlands',
      fr: 'Pays-Bas',
      'zh-CN': '荷兰',
    },
    region: 'Europe',
  },
  prg: {
    id: 'prg',
    code: 'PRG',
    apiCode: 'prg',
    flag: '🇨🇿',
    countryCode: 'cz',
    city: {
      en: 'Prague',
      fr: 'Prague',
      'zh-CN': '布拉格',
    },
    country: {
      en: 'Czechia',
      fr: 'Tchéquie',
      'zh-CN': '捷克',
    },
    region: 'Europe',
  },
};

const ALIAS_MAP: Record<string, string> = {
  ynm: 'mum',
  'ca-east-tor': 'tor',
  'ca-east-tor-a': 'tor',
  'ca-east-tor-b': 'tor',
  'ca-east-bhs': 'bhs',
  'eu-west-gra': 'gra',
  'eu-west-rbx': 'rbx',
  'eu-west-sbg': 'sbg',
  'eu-west-par': 'par',
  'eu-south-mrs': 'mrs',
  'eu-west-lim': 'lim',
  'eu-central-fra': 'fra',
  'eu-west-eri': 'eri',
  'eu-west-lon': 'lon',
  'eu-central-waw': 'waw',
  'us-east-vin': 'vin',
  'us-west-hil': 'hil',
  'ap-southeast-sgp': 'sgp',
  'ap-southeast-syd': 'syd',
  'ap-south-mum': 'mum',
  'ap-south-ynm': 'mum',
  'eu-west-lz-bru': 'bru',
  'eu-south-lz-mad': 'mad',
  'eu-south-lz-mil': 'mil',
  'eu-central-lz-zur': 'zur',
  'eu-west-lz-ams': 'ams',
  'eu-central-lz-prg': 'prg',
};

/**
 * Normalizes any raw OVH datacenter code (e.g. 'ca-east-tor-a', 'gra1', 'rbx-a', 'ynm')
 * to its canonical lowercase code (e.g. 'tor', 'gra', 'rbx', 'mum').
 */
export function normalizeDatacenterCode(rawCode: string): string {
  if (!rawCode) return '';
  const low = rawCode.trim().toLowerCase();
  if (!low) return '';

  if (DATACENTER_DICTIONARY[low]) return low;
  if (ALIAS_MAP[low]) return ALIAS_MAP[low];

  for (const [aliasPrefix, canonical] of Object.entries(ALIAS_MAP)) {
    if (low.startsWith(aliasPrefix)) {
      return canonical;
    }
  }

  // Match 3-letter prefix followed by digits or hyphen (e.g. gra1, rbx-a, bhs5, ynm1)
  const prefix3 = low.slice(0, 3);
  if (prefix3 === 'ynm') return 'mum';
  if (DATACENTER_DICTIONARY[prefix3]) {
    const rest = low.slice(3);
    if (!rest || /^[-_\d]/.test(rest)) {
      return prefix3;
    }
  }

  // Check if any known datacenter code appears as a hyphen-delimited token (e.g. eu-west-lz-bru-a)
  const tokens = low.split(/[-_]/);
  for (let i = tokens.length - 1; i >= 0; i--) {
    const tok = tokens[i];
    if (tok === 'ynm') return 'mum';
    if (DATACENTER_DICTIONARY[tok]) return tok;
  }

  return low;
}

/**
 * Resolves tri-lingual metadata for a datacenter code.
 * Always returns a valid DatacenterMeta object even for unknown codes.
 */
export function getDatacenterMeta(rawCode: string): DatacenterMeta {
  const norm = normalizeDatacenterCode(rawCode);
  if (norm && DATACENTER_DICTIONARY[norm]) {
    return DATACENTER_DICTIONARY[norm];
  }
  const cleanCode = (rawCode || 'UNKNOWN').trim().toUpperCase();
  return {
    id: norm || cleanCode.toLowerCase(),
    code: cleanCode,
    apiCode: (rawCode || '').trim().toLowerCase(),
    flag: '🌐',
    countryCode: '',
    city: {
      en: cleanCode,
      fr: cleanCode,
      'zh-CN': cleanCode,
    },
    country: {
      en: 'Global',
      fr: 'Global',
      'zh-CN': '全球',
    },
    region: 'Europe',
  };
}

/**
 * Formats a datacenter code into either:
 * - 'full':
 *   - zh-CN: `BHS（博阿努瓦，加拿大 🇨🇦）` (or `SGP（新加坡 🇸🇬）` when city === country)
 *   - en/fr: `BHS (Beauharnois, Canada 🇨🇦)` (or `SGP (Singapore 🇸🇬)` when city === country)
 * - 'compact': `🇨🇦 BHS`
 */
export function formatDatacenter(
  rawCode: string,
  locale: Locale = 'zh-CN',
  mode: 'full' | 'compact' = 'full'
): string {
  if (!rawCode || !rawCode.trim()) return '-';
  const meta = getDatacenterMeta(rawCode);
  if (mode === 'compact') {
    return `${meta.flag} ${meta.code}`.trim();
  }

  const city = meta.city[locale] || meta.city.en;
  const country = meta.country[locale] || meta.country.en;
  const sameCityCountry =
    city.toLowerCase() === country.toLowerCase() || meta.id === 'sgp';

  if (locale === 'zh-CN') {
    const locationPart = sameCityCountry ? city : `${city}，${country}`;
    return `${meta.code}（${locationPart} ${meta.flag}）`;
  }

  const locationPart = sameCityCountry ? city : `${city}, ${country}`;
  return `${meta.code} (${locationPart} ${meta.flag})`;
}

export const formatDC = formatDatacenter;

/**
 * Infers the hardware generation year (e.g. 2024, 2026, 2027) from an OVH planCode prefix.
 * Returns null if no 2-digit generation prefix is present.
 */
export function inferPlanGeneration(planCode: string, explicitGeneration?: number): number | null {
  if (typeof explicitGeneration === 'number' && explicitGeneration >= 2020 && explicitGeneration <= 2035) {
    return explicitGeneration;
  }
  if (!planCode) return null;
  const m = planCode.trim().match(/^(\d{2})[a-z]/i);
  if (!m) return null;
  const yy = parseInt(m[1], 10);
  if (yy >= 20 && yy <= 35) {
    return 2000 + yy;
  }
  return null;
}
