import {
  DATACENTER_DICTIONARY,
  getDatacenterMeta,
  formatDatacenter,
  formatDC,
  normalizeDatacenterCode,
  inferPlanGeneration,
  type DatacenterMeta,
  type DatacenterRegion,
} from './datacenters';

export {
  DATACENTER_DICTIONARY,
  getDatacenterMeta,
  formatDatacenter,
  formatDC,
  normalizeDatacenterCode,
  inferPlanGeneration,
  type DatacenterMeta,
  type DatacenterRegion,
};

export interface DatacenterInfo {
  code: string;
  name: string;
  region: string;
  flag: string;
  countryCode: string;
  meta: DatacenterMeta;
}

const PRIMARY_DC_ORDER = [
  'gra',
  'sbg',
  'rbx',
  'par',
  'mrs',
  'lim',
  'fra',
  'lon',
  'eri',
  'waw',
  'bhs',
  'tor',
  'hil',
  'vin',
  'sgp',
  'syd',
  'mum',
] as const;

export const OVH_DATACENTERS: DatacenterInfo[] = PRIMARY_DC_ORDER.map((id) => {
  const meta = getDatacenterMeta(id);
  return {
    code: meta.id,
    name: meta.city['zh-CN'],
    region: meta.country['zh-CN'],
    flag: meta.flag,
    countryCode: meta.countryCode,
    meta,
  };
});
