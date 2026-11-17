package catalog

import (
	"fmt"
	"math"
	"regexp"
	"strconv"
	"strings"
)

// RegionTag describes a regional Asia-Pacific or geographic suffix attached to an OVH planCode.
type RegionTag struct {
	Code string `json:"code"`
	ZHCN string `json:"zh-CN"`
	EN   string `json:"en"`
	FR   string `json:"fr"`
}

// CPUInfo captures structured processor specifications from OVH technical blobs.
type CPUInfo struct {
	Brand     string  `json:"brand,omitempty"`
	Model     string  `json:"model,omitempty"`
	Cores     int     `json:"cores,omitempty"`
	Threads   int     `json:"threads,omitempty"`
	Frequency float64 `json:"frequency,omitempty"`
	Boost     float64 `json:"boost,omitempty"`
}

// PlanMeta summarizes commercial name, range, CPU, and regional metadata for a planCode.
type PlanMeta struct {
	PlanCode      string             `json:"planCode"`
	BaseCode      string             `json:"baseCode"`
	Name          string             `json:"name"`
	Range         string             `json:"range"`
	Generation    int                `json:"generation,omitempty"`
	CPU           *CPUInfo           `json:"cpu,omitempty"`
	Region        *RegionTag         `json:"region,omitempty"`
	Price         *float64           `json:"price,omitempty"`
	Currency      string             `json:"currency,omitempty"`
	AddonPrices   map[string]float64 `json:"addonPrices,omitempty"`
	CatalogSource string             `json:"catalogSource,omitempty"`
}

// ParsedMemory holds structured fields extracted from an OVH ram-* code.
type ParsedMemory struct {
	Raw       string `json:"raw"`
	SizeGB    int    `json:"sizeGB"`
	RAMType   string `json:"ramType"`
	ECC       string `json:"ecc"`
	Frequency int    `json:"frequency"`
}

// StorageDiskGroup describes one disk group within an OVH storage code.
type StorageDiskGroup struct {
	Count      int    `json:"count"`
	CapacityGB int    `json:"capacityGB"`
	Type       string `json:"type"`
	PCIe       string `json:"pcie"`
}

// ParsedStorage holds structured fields extracted from an OVH storage/systemStorage code.
type ParsedStorage struct {
	Raw      string             `json:"raw"`
	None     bool               `json:"none"`
	RAID     string             `json:"raid"`
	IsSystem bool               `json:"isSystem"`
	Groups   []StorageDiskGroup `json:"groups"`
}

// ParsedGPU holds structured fields extracted from an OVH gpu-* code.
type ParsedGPU struct {
	Raw    string `json:"raw"`
	Count  int    `json:"count"`
	Vendor string `json:"vendor"`
	Model  string `json:"model"`
	VRAMGB int    `json:"vramGB"`
}

var regionSuffixMap = map[string]RegionTag{
	"sgp":  {Code: "SGP", ZHCN: "新加坡", EN: "Singapore", FR: "Singapour"},
	"syd":  {Code: "SYD", ZHCN: "悉尼", EN: "Sydney", FR: "Sydney"},
	"mum":  {Code: "MUM", ZHCN: "孟买", EN: "Mumbai", FR: "Mumbai"},
	"ynm":  {Code: "MUM", ZHCN: "孟买", EN: "Mumbai", FR: "Mumbai"},
	"tor":  {Code: "TOR", ZHCN: "多伦多", EN: "Toronto", FR: "Toronto"},
	"apac": {Code: "APAC", ZHCN: "亚太", EN: "APAC", FR: "APAC"},
}

// DatacenterInfo holds tri-lingual city, country, region, ISO country code, and flag emoji for an OVH datacenter.
type DatacenterInfo struct {
	Code        string `json:"code"`
	Name        string `json:"name"`
	Region      string `json:"region"`
	CountryCode string `json:"countryCode"`
	Flag        string `json:"flag"`
	CityEN      string `json:"cityEn"`
	CityFR      string `json:"cityFr"`
	CityZHCN    string `json:"cityZhCn"`
	CountryEN   string `json:"countryEn"`
	CountryFR   string `json:"countryFr"`
	CountryZHCN string `json:"countryZhCn"`
}

var datacenterMetaMap = map[string]DatacenterInfo{
	"bhs": {
		Code: "BHS", Name: "Beauharnois", Region: "Canada", CountryCode: "CA", Flag: "🇨🇦",
		CityEN: "Beauharnois", CityFR: "Beauharnois", CityZHCN: "博阿努瓦",
		CountryEN: "Canada", CountryFR: "Canada", CountryZHCN: "加拿大",
	},
	"tor": {
		Code: "TOR", Name: "Toronto", Region: "Canada", CountryCode: "CA", Flag: "🇨🇦",
		CityEN: "Toronto", CityFR: "Toronto", CityZHCN: "多伦多",
		CountryEN: "Canada", CountryFR: "Canada", CountryZHCN: "加拿大",
	},
	"ca-east-tor": {
		Code: "TOR", Name: "Toronto", Region: "Canada", CountryCode: "CA", Flag: "🇨🇦",
		CityEN: "Toronto", CityFR: "Toronto", CityZHCN: "多伦多",
		CountryEN: "Canada", CountryFR: "Canada", CountryZHCN: "加拿大",
	},
	"ca-east-tor-a": {
		Code: "TOR", Name: "Toronto", Region: "Canada", CountryCode: "CA", Flag: "🇨🇦",
		CityEN: "Toronto", CityFR: "Toronto", CityZHCN: "多伦多",
		CountryEN: "Canada", CountryFR: "Canada", CountryZHCN: "加拿大",
	},
	"ca-east-tor-b": {
		Code: "TOR", Name: "Toronto", Region: "Canada", CountryCode: "CA", Flag: "🇨🇦",
		CityEN: "Toronto", CityFR: "Toronto", CityZHCN: "多伦多",
		CountryEN: "Canada", CountryFR: "Canada", CountryZHCN: "加拿大",
	},
	"gra": {
		Code: "GRA", Name: "Gravelines", Region: "France", CountryCode: "FR", Flag: "🇫🇷",
		CityEN: "Gravelines", CityFR: "Gravelines", CityZHCN: "格拉沃利讷",
		CountryEN: "France", CountryFR: "France", CountryZHCN: "法国",
	},
	"rbx": {
		Code: "RBX", Name: "Roubaix", Region: "France", CountryCode: "FR", Flag: "🇫🇷",
		CityEN: "Roubaix", CityFR: "Roubaix", CityZHCN: "鲁贝",
		CountryEN: "France", CountryFR: "France", CountryZHCN: "法国",
	},
	"sbg": {
		Code: "SBG", Name: "Strasbourg", Region: "France", CountryCode: "FR", Flag: "🇫🇷",
		CityEN: "Strasbourg", CityFR: "Strasbourg", CityZHCN: "斯特拉斯堡",
		CountryEN: "France", CountryFR: "France", CountryZHCN: "法国",
	},
	"par": {
		Code: "PAR", Name: "Paris", Region: "France", CountryCode: "FR", Flag: "🇫🇷",
		CityEN: "Paris", CityFR: "Paris", CityZHCN: "巴黎",
		CountryEN: "France", CountryFR: "France", CountryZHCN: "法国",
	},
	"eu-west-par": {
		Code: "PAR", Name: "Paris", Region: "France", CountryCode: "FR", Flag: "🇫🇷",
		CityEN: "Paris", CityFR: "Paris", CityZHCN: "巴黎",
		CountryEN: "France", CountryFR: "France", CountryZHCN: "法国",
	},
	"mrs": {
		Code: "MRS", Name: "Marseille", Region: "France", CountryCode: "FR", Flag: "🇫🇷",
		CityEN: "Marseille", CityFR: "Marseille", CityZHCN: "马赛",
		CountryEN: "France", CountryFR: "France", CountryZHCN: "法国",
	},
	"eu-south-mrs": {
		Code: "MRS", Name: "Marseille", Region: "France", CountryCode: "FR", Flag: "🇫🇷",
		CityEN: "Marseille", CityFR: "Marseille", CityZHCN: "马赛",
		CountryEN: "France", CountryFR: "France", CountryZHCN: "法国",
	},
	"fra": {
		Code: "FRA", Name: "Frankfurt", Region: "Germany", CountryCode: "DE", Flag: "🇩🇪",
		CityEN: "Frankfurt", CityFR: "Francfort", CityZHCN: "法兰克福",
		CountryEN: "Germany", CountryFR: "Allemagne", CountryZHCN: "德国",
	},
	"lim": {
		Code: "LIM", Name: "Limburg", Region: "Germany", CountryCode: "DE", Flag: "🇩🇪",
		CityEN: "Limburg", CityFR: "Limbourg", CityZHCN: "林堡",
		CountryEN: "Germany", CountryFR: "Allemagne", CountryZHCN: "德国",
	},
	"lon": {
		Code: "LON", Name: "London", Region: "United Kingdom", CountryCode: "GB", Flag: "🇬🇧",
		CityEN: "London", CityFR: "Londres", CityZHCN: "伦敦",
		CountryEN: "UK", CountryFR: "Royaume-Uni", CountryZHCN: "英国",
	},
	"eri": {
		Code: "ERI", Name: "Erith", Region: "United Kingdom", CountryCode: "GB", Flag: "🇬🇧",
		CityEN: "Erith", CityFR: "Erith", CityZHCN: "埃里斯",
		CountryEN: "UK", CountryFR: "Royaume-Uni", CountryZHCN: "英国",
	},
	"waw": {
		Code: "WAW", Name: "Warsaw", Region: "Poland", CountryCode: "PL", Flag: "🇵🇱",
		CityEN: "Warsaw", CityFR: "Varsovie", CityZHCN: "华沙",
		CountryEN: "Poland", CountryFR: "Pologne", CountryZHCN: "波兰",
	},
	"vin": {
		Code: "VIN", Name: "Vint Hill", Region: "United States (East)", CountryCode: "US", Flag: "🇺🇸",
		CityEN: "Vint Hill, Virginia", CityFR: "Vint Hill, Virginie", CityZHCN: "弗吉尼亚温特山",
		CountryEN: "USA", CountryFR: "États-Unis", CountryZHCN: "美国",
	},
	"hil": {
		Code: "HIL", Name: "Hillsboro", Region: "United States (West)", CountryCode: "US", Flag: "🇺🇸",
		CityEN: "Hillsboro, Oregon", CityFR: "Hillsboro, Oregon", CityZHCN: "俄勒冈希尔斯伯勒",
		CountryEN: "USA", CountryFR: "États-Unis", CountryZHCN: "美国",
	},
	"syd": {
		Code: "SYD", Name: "Sydney", Region: "Australia", CountryCode: "AU", Flag: "🇦🇺",
		CityEN: "Sydney", CityFR: "Sydney", CityZHCN: "悉尼",
		CountryEN: "Australia", CountryFR: "Australie", CountryZHCN: "澳大利亚",
	},
	"sgp": {
		Code: "SGP", Name: "Singapore", Region: "Singapore", CountryCode: "SG", Flag: "🇸🇬",
		CityEN: "Singapore", CityFR: "Singapour", CityZHCN: "新加坡",
		CountryEN: "Singapore", CountryFR: "Singapour", CountryZHCN: "新加坡",
	},
	"mum": {
		Code: "MUM", Name: "Mumbai", Region: "India", CountryCode: "IN", Flag: "🇮🇳",
		CityEN: "Mumbai", CityFR: "Mumbai", CityZHCN: "孟买",
		CountryEN: "India", CountryFR: "Inde", CountryZHCN: "印度",
	},
	"ynm": {
		Code: "MUM", Name: "Mumbai", Region: "India", CountryCode: "IN", Flag: "🇮🇳",
		CityEN: "Mumbai", CityFR: "Mumbai", CityZHCN: "孟买",
		CountryEN: "India", CountryFR: "Inde", CountryZHCN: "印度",
	},
	"bru": {
		Code: "BRU", Name: "Brussels", Region: "Belgium", CountryCode: "BE", Flag: "🇧🇪",
		CityEN: "Brussels", CityFR: "Bruxelles", CityZHCN: "布鲁塞尔",
		CountryEN: "Belgium", CountryFR: "Belgique", CountryZHCN: "比利时",
	},
	"mad": {
		Code: "MAD", Name: "Madrid", Region: "Spain", CountryCode: "ES", Flag: "🇪🇸",
		CityEN: "Madrid", CityFR: "Madrid", CityZHCN: "马德里",
		CountryEN: "Spain", CountryFR: "Espagne", CountryZHCN: "西班牙",
	},
	"mil": {
		Code: "MIL", Name: "Milan", Region: "Italy", CountryCode: "IT", Flag: "🇮🇹",
		CityEN: "Milan", CityFR: "Milan", CityZHCN: "米兰",
		CountryEN: "Italy", CountryFR: "Italie", CountryZHCN: "意大利",
	},
	"zur": {
		Code: "ZUR", Name: "Zurich", Region: "Switzerland", CountryCode: "CH", Flag: "🇨🇭",
		CityEN: "Zurich", CityFR: "Zurich", CityZHCN: "苏黎世",
		CountryEN: "Switzerland", CountryFR: "Suisse", CountryZHCN: "瑞士",
	},
	"ams": {
		Code: "AMS", Name: "Amsterdam", Region: "Netherlands", CountryCode: "NL", Flag: "🇳🇱",
		CityEN: "Amsterdam", CityFR: "Amsterdam", CityZHCN: "阿姆斯特丹",
		CountryEN: "Netherlands", CountryFR: "Pays-Bas", CountryZHCN: "荷兰",
	},
	"prg": {
		Code: "PRG", Name: "Prague", Region: "Czechia", CountryCode: "CZ", Flag: "🇨🇿",
		CityEN: "Prague", CityFR: "Prague", CityZHCN: "布拉格",
		CountryEN: "Czechia", CountryFR: "Tchéquie", CountryZHCN: "捷克",
	},
}

var (
	reRegionSuffix   = regexp.MustCompile(`(?i)-(sgp|syd|mum|ynm|tor|apac)$`)
	reVersionSuffix  = regexp.MustCompile(`(?i)-v\d+$`)
	reTrailingVer    = regexp.MustCompile(`^(\d{2}[a-z]+\d{2})\d$`)
	rePlanBody       = regexp.MustCompile(`^(\d{2})([a-z0-9-]+)$`)
	rePlanGeneration = regexp.MustCompile(`(?i)^(\d{2})[a-z]`)
	reParenCode      = regexp.MustCompile(`\(([a-zA-Z0-9-]+)\)\s*$`)
	reDigitsOnly    = regexp.MustCompile(`^(\d+)([a-z]*)$`)
	reRiseSize      = regexp.MustCompile(`^(s|m|l|xl)(\d+)$`)
	reScaleCode     = regexp.MustCompile(`^scale(amd|intel|gpu)(\d+)$`)
	reMemoryCode    = regexp.MustCompile(`(?i)^ram-(\d+)g(?:-(ddr\d))?(?:-(on-die-ecc|noecc|ecc))?(?:-(\d+))?$`)
	reMemoryAny     = regexp.MustCompile(`(?i)ram-(\d+)g`)
	reStorageGroup  = regexp.MustCompile(`(\d+)x(\d+)(?:-(hdd))?-?(sas|sa|sata|ssd|nvme)(?:-(sata|sas))?(?:-(pcie-gen\d))?`)
	reGPUCode       = regexp.MustCompile(`(?i)^gpu-(\d+)x([a-z0-9-]+?)(?:-(\d+)g)?$`)
	reRXModel       = regexp.MustCompile(`^RX(\d+)(XT)?$`)
	reTrafficBW     = regexp.MustCompile(`(?i)traffic-(\d+)(tb|gb|mb)-(\d+)`)
	reTrafficOnly   = regexp.MustCompile(`(?i)traffic-(\d+)(tb|gb|mb)(?:$|[-\s])`)
	reBandwidthNum  = regexp.MustCompile(`(?i)bandwidth-(\d+)`)
	reVRackBW       = regexp.MustCompile(`(?i)vrack-bandwidth-(\d+)`)
	reFirstDigits   = regexp.MustCompile(`(\d+)`)

	standardizePatterns = []*regexp.Regexp{
		regexp.MustCompile(`-\d+skl[a-e]\d{2}(-v\d+)?`),
		regexp.MustCompile(`-\d+sk\d+`),
		regexp.MustCompile(`-\d+rise\d*`),
		regexp.MustCompile(`-\d+sys\w*`),
		regexp.MustCompile(`-\d+risegame\d*`),
		regexp.MustCompile(`-\d+risestor`),
		regexp.MustCompile(`-\d+skgame\d*`),
		regexp.MustCompile(`-\d+ska\d*`),
		regexp.MustCompile(`-\d+skstor\d*`),
		regexp.MustCompile(`-\d+sysstor`),
		regexp.MustCompile(`game\d*`),
		regexp.MustCompile(`stor\d*`),
		regexp.MustCompile(`-ks\d+`),
		regexp.MustCompile(`-rise`),
		regexp.MustCompile(`-\d+sysle\d+`),
		regexp.MustCompile(`-\d+skb\d+`),
		regexp.MustCompile(`-\d+skc\d+`),
		regexp.MustCompile(`-\d+sk\d+b`),
		regexp.MustCompile(`-v\d+`),
		regexp.MustCompile(`-[a-z]{3}$`),
	}
	reStdMemECC  = regexp.MustCompile(`-(no)?ecc-\d+`)
	reStdStorage = regexp.MustCompile(`-(sas|sa|ssd|nvme)$`)
	reStdFreq    = regexp.MustCompile(`-\d{4,5}$`)
)

// NormalizeDisplayDatacenter maps OVH internal API datacenter codes (e.g. ynm, ca-east-tor-a) to UI display codes (mum, tor).
func NormalizeDisplayDatacenter(dc string) string {
	low := strings.ToLower(strings.TrimSpace(dc))
	switch {
	case low == "ynm":
		return "mum"
	case low == "ca-east-tor" || strings.HasPrefix(low, "ca-east-tor-"):
		return "tor"
	case low == "eu-west-par" || strings.HasPrefix(low, "eu-west-par-"):
		return "par"
	case low == "eu-south-mrs" || strings.HasPrefix(low, "eu-south-mrs-"):
		return "mrs"
	case strings.HasPrefix(low, "eu-west-lz-"):
		suffix := strings.TrimPrefix(low, "eu-west-lz-")
		if len(suffix) >= 3 {
			if _, ok := datacenterMetaMap[suffix[:3]]; ok {
				return suffix[:3]
			}
		}
	}
	return low
}

// NormalizeAPIDatacenter maps UI display datacenter codes (e.g. mum) to OVH API codes (ynm).
func NormalizeAPIDatacenter(dc string) string {
	low := strings.ToLower(strings.TrimSpace(dc))
	if low == "" {
		return "gra"
	}
	if low == "mum" {
		return "ynm"
	}
	return low
}

// LookupDatacenterInfo resolves structured tri-lingual DatacenterInfo for a raw or normalized datacenter code.
func LookupDatacenterInfo(dcCode string) (DatacenterInfo, bool) {
	raw := strings.TrimSpace(dcCode)
	if raw == "" {
		return DatacenterInfo{}, false
	}
	low := strings.ToLower(raw)
	if m, ok := datacenterMetaMap[low]; ok {
		return m, true
	}
	norm := NormalizeDisplayDatacenter(low)
	if m, ok := datacenterMetaMap[norm]; ok {
		return m, true
	}
	if m := reParenCode.FindStringSubmatch(raw); len(m) == 2 {
		inner := NormalizeDisplayDatacenter(m[1])
		if info, ok := datacenterMetaMap[inner]; ok {
			return info, true
		}
	}
	if strings.Contains(norm, "-") {
		parts := strings.Split(norm, "-")
		for i := len(parts) - 1; i >= 0; i-- {
			p := parts[i]
			if len(p) >= 3 {
				if info, ok := datacenterMetaMap[p[:3]]; ok {
					return info, true
				}
			}
		}
	}
	if len(norm) > 3 {
		if info, ok := datacenterMetaMap[norm[:3]]; ok {
			return info, true
		}
	}
	return DatacenterInfo{}, false
}

// DatacenterMeta returns (dcName, region, countryCode) for a datacenter code.
func DatacenterMeta(dcCode string) (string, string, string) {
	if info, ok := LookupDatacenterInfo(dcCode); ok {
		return info.Name, info.Region, info.CountryCode
	}
	low := strings.ToLower(strings.TrimSpace(dcCode))
	if low == "" {
		return "Unknown", "Unknown", ""
	}
	return strings.ToUpper(low), "Global", ""
}

// DatacenterFlag returns the flag emoji for a datacenter code, or empty string if unknown.
func DatacenterFlag(dcCode string) string {
	if info, ok := LookupDatacenterInfo(dcCode); ok {
		return info.Flag
	}
	return ""
}

// FormatDatacenterLabel formats a datacenter code with localized city, country, and flag emoji
// (e.g. "BHS（博阿努瓦，加拿大 🇨🇦）" for zh-CN, "BHS (Beauharnois, Canada 🇨🇦)" for en/fr).
func FormatDatacenterLabel(dcCode, locale string) string {
	raw := strings.TrimSpace(dcCode)
	if raw == "" {
		return ""
	}
	if strings.Contains(raw, ",") {
		parts := strings.Split(raw, ",")
		formatted := make([]string, 0, len(parts))
		for _, p := range parts {
			if t := strings.TrimSpace(p); t != "" {
				formatted = append(formatted, FormatDatacenterLabel(t, locale))
			}
		}
		if len(formatted) > 0 {
			return strings.Join(formatted, ", ")
		}
	}

	info, ok := LookupDatacenterInfo(raw)
	if !ok {
		return strings.ToUpper(raw)
	}

	loc := strings.ToLower(strings.TrimSpace(locale))
	isZH := strings.HasPrefix(loc, "zh")
	isFR := strings.HasPrefix(loc, "fr")

	city := info.CityEN
	country := info.CountryEN
	if isZH {
		city = nonEmpty(info.CityZHCN, city)
		country = nonEmpty(info.CountryZHCN, country)
	} else if isFR {
		city = nonEmpty(info.CityFR, city)
		country = nonEmpty(info.CountryFR, country)
	}

	location := city
	if country != "" && !strings.EqualFold(city, country) {
		if isZH {
			location = fmt.Sprintf("%s，%s", city, country)
		} else {
			location = fmt.Sprintf("%s, %s", city, country)
		}
	}
	if info.Flag != "" {
		location = fmt.Sprintf("%s %s", location, info.Flag)
	}
	if isZH {
		return fmt.Sprintf("%s（%s）", info.Code, location)
	}
	return fmt.Sprintf("%s (%s)", info.Code, location)
}

// InferCartRegion infers the OVH cart item `region` configuration value for a datacenter.
func InferCartRegion(apiDatacenter string) string {
	dc := strings.ToLower(strings.TrimSpace(apiDatacenter))
	if strings.HasPrefix(dc, "bhs") || strings.HasPrefix(dc, "tor") || strings.HasPrefix(dc, "ca-east-tor") || strings.HasPrefix(dc, "ca-") {
		return "canada"
	}
	for _, p := range []string{"gra", "rbx", "sbg", "eri", "lim", "waw", "par", "fra", "lon", "mrs", "bru", "mad", "mil", "zur", "ams", "prg", "eu-"} {
		if strings.HasPrefix(dc, p) {
			return "europe"
		}
	}
	if strings.HasPrefix(dc, "vin") || strings.HasPrefix(dc, "hil") || strings.HasPrefix(dc, "us-") {
		return "usa"
	}
	if strings.HasPrefix(dc, "syd") || strings.HasPrefix(dc, "sgp") || strings.HasPrefix(dc, "ynm") || strings.HasPrefix(dc, "mum") || strings.HasPrefix(dc, "ap-") {
		return "apac"
	}
	return ""
}

// InferPlanGeneration extracts the 4-digit hardware generation year (2020..2035) from a planCode prefix (e.g. "24ska01" -> 2024).
func InferPlanGeneration(planCode string) int {
	m := rePlanGeneration.FindStringSubmatch(strings.TrimSpace(planCode))
	if len(m) != 2 {
		return 0
	}
	yy, err := strconv.Atoi(m[1])
	if err != nil || yy < 20 || yy > 35 {
		return 0
	}
	return 2000 + yy
}

// IsNonServerAddonCode reports whether a planCode looks like a hardware addon, license, or OS option rather than a server planCode.
func IsNonServerAddonCode(code string) bool {
	low := strings.ToLower(strings.TrimSpace(code))
	if low == "" || isLicenseOrOSOption(low) {
		return true
	}
	for _, prefix := range []string{
		"ram-", "softraid-", "hardraid-", "hybridsoftraid-", "noraid-",
		"bandwidth-", "vrack-", "traffic-", "gpu-", "os-",
	} {
		if strings.HasPrefix(low, prefix) {
			return true
		}
	}
	return false
}

// ParsePlanRegion extracts the base planCode (with regional suffix removed, preserving version suffix)
// and optional regional suffix tag.
func ParsePlanRegion(planCode string) (string, *RegionTag) {
	code := strings.TrimSpace(planCode)
	if code == "" {
		return "", nil
	}
	var regTag *RegionTag
	if loc := reRegionSuffix.FindStringSubmatchIndex(code); loc != nil {
		key := strings.ToLower(code[loc[2]:loc[3]])
		if tag, ok := regionSuffixMap[key]; ok {
			copyTag := tag
			regTag = &copyTag
		}
		code = code[:loc[0]]
	}
	return code, regTag
}

// StripPlanVersion removes both the regional suffix and any trailing -vN version suffix from planCode.
func StripPlanVersion(planCode string) string {
	baseCode, _ := ParsePlanRegion(planCode)
	return reVersionSuffix.ReplaceAllString(baseCode, "")
}

// BuildConfigKey constructs a canonical configuration fingerprint from memory and storage codes.
func BuildConfigKey(memory, storage string) string {
	mem := strings.TrimSpace(memory)
	sto := strings.TrimSpace(storage)
	if strings.EqualFold(mem, "N/A") || strings.EqualFold(mem, "none") {
		mem = ""
	}
	if strings.EqualFold(sto, "N/A") || strings.EqualFold(sto, "none") {
		sto = ""
	}
	switch {
	case mem != "" && sto != "":
		return mem + "+" + sto
	case mem != "":
		return mem
	case sto != "":
		return sto
	default:
		return "default"
	}
}

// InferPlanFromCode resolves a commercial name and product range deterministically from a planCode.
func InferPlanFromCode(planCode string) PlanMeta {
	baseCode, regionInfo := ParsePlanRegion(planCode)
	noVer := reVersionSuffix.ReplaceAllString(baseCode, "")
	clean := reTrailingVer.ReplaceAllString(strings.ToLower(noVer), "$1")

	body := clean
	if m := rePlanBody.FindStringSubmatch(clean); len(m) == 3 {
		body = m[2]
	}

	name := strings.ToUpper(strings.TrimSpace(planCode))
	productRange := "OTHER"

	switch {
	case strings.HasPrefix(body, "ska"):
		idx := strings.TrimLeft(body[3:], "0")
		if body == "ska01" {
			name = "KS-A"
		} else {
			if idx == "" {
				idx = "A"
			}
			name = "KS-A" + idx
		}
		productRange = "KS"
	case strings.HasPrefix(body, "skb"):
		idx := strings.TrimLeft(body[3:], "0")
		if body == "skb01" {
			name = "KS-B"
		} else {
			name = "KS-B" + idx
		}
		productRange = "KS"
	case strings.HasPrefix(body, "skc"):
		idx := strings.TrimLeft(body[3:], "0")
		if body == "skc01" {
			name = "KS-C"
		} else {
			name = "KS-C" + idx
		}
		productRange = "KS"
	case strings.HasPrefix(body, "skle"):
		num := strings.TrimLeft(body[4:], "0")
		if num == "" {
			num = "1"
		}
		name = "KS-LE-" + num
		productRange = "KS"
	case strings.HasPrefix(body, "skgame"):
		num := strings.TrimLeft(body[6:], "0")
		if num == "" {
			num = "1"
		}
		name = "KS-GAME-" + num
		productRange = "KS"
	case strings.HasPrefix(body, "skstor"):
		num := strings.TrimLeft(body[6:], "0")
		if num == "" {
			num = "1"
		}
		name = "KS-STOR-" + num
		productRange = "KS"
	case strings.HasPrefix(body, "sk"):
		rest := body[2:]
		if m := reDigitsOnly.FindStringSubmatch(rest); len(m) == 3 {
			n, _ := strconv.Atoi(m[1])
			suffix := strings.ToUpper(m[2])
			if n >= 10 && n%10 == 0 {
				n /= 10
			}
			if suffix != "" {
				name = fmt.Sprintf("KS-%d-%s", n, suffix)
			} else {
				name = fmt.Sprintf("KS-%d", n)
			}
		}
		productRange = "KS"
	case strings.HasPrefix(body, "risele"):
		num := strings.TrimLeft(body[6:], "0")
		if num == "" {
			num = "1"
		}
		name = "RISE-LE-" + num
		productRange = "RISE"
	case strings.HasPrefix(body, "risegame"):
		num := strings.TrimLeft(body[8:], "0")
		if num == "" {
			num = "1"
		}
		name = "RISE-GAME-" + num
		productRange = "RISE"
	case strings.HasPrefix(body, "risestor"):
		num := strings.TrimLeft(body[8:], "0")
		if num == "" || num == "1" {
			name = "RISE-STOR"
		} else {
			name = "RISE-STOR-" + num
		}
		productRange = "RISE"
	case strings.HasPrefix(body, "rise"):
		rest := body[4:]
		if m := reRiseSize.FindStringSubmatch(rest); len(m) == 3 {
			name = "RISE-" + strings.ToUpper(m[1])
		} else {
			num := strings.TrimLeft(rest, "0")
			if num == "" {
				num = "1"
			}
			name = "RISE-" + num
		}
		productRange = "RISE"
	case strings.HasPrefix(body, "sysstorle"):
		num := strings.TrimLeft(body[9:], "0")
		if num == "" {
			num = "1"
		}
		name = "SYS-STOR-LE-" + num
		productRange = "SYS"
	case strings.HasPrefix(body, "sysle"):
		num := strings.TrimLeft(body[5:], "0")
		if num == "" {
			num = "1"
		}
		name = "SYS-LE-" + num
		productRange = "SYS"
	case strings.HasPrefix(body, "sysgame"):
		num := strings.TrimLeft(body[7:], "0")
		if num == "" {
			num = "1"
		}
		name = "SYS-GAME-" + num
		productRange = "SYS"
	case strings.HasPrefix(body, "sysstor"):
		num := strings.TrimLeft(body[7:], "0")
		if num == "" || num == "1" {
			name = "SYS-STOR"
		} else {
			name = "SYS-STOR-" + num
		}
		productRange = "SYS"
	case strings.HasPrefix(body, "sys"):
		rest := strings.TrimLeft(body[3:], "0")
		if rest == "" {
			rest = "1"
		}
		name = "SYS-" + strings.ToUpper(rest)
		productRange = "SYS"
	case strings.HasPrefix(body, "advstor"):
		num := strings.TrimLeft(body[7:], "0")
		if num == "" {
			num = "1"
		}
		name = "ADVANCE-STOR-" + num
		productRange = "ADV"
	case strings.HasPrefix(body, "adv"):
		num := strings.TrimLeft(body[3:], "0")
		if num == "" {
			num = "1"
		}
		name = "ADVANCE-" + num
		productRange = "ADV"
	case strings.HasPrefix(body, "scale"):
		if m := reScaleCode.FindStringSubmatch(body); len(m) == 3 {
			archMap := map[string]string{"amd": "a", "intel": "i", "gpu": "gpu-"}
			num, _ := strconv.Atoi(m[2])
			name = fmt.Sprintf("SCALE-%s%d", archMap[m[1]], num)
		} else {
			name = strings.ToUpper(body)
		}
		productRange = "SCALE"
	case strings.HasPrefix(body, "hci") || strings.HasPrefix(body, "hgr") || strings.HasPrefix(body, "hg") || strings.HasPrefix(body, "sds"):
		name = "HGR-" + strings.ToUpper(body)
		productRange = "HGR"
	case strings.HasPrefix(body, "game"):
		num := strings.TrimLeft(body[4:], "0")
		if num == "" {
			num = "1"
		}
		name = "GAME-" + num
		productRange = "ADV"
	case strings.HasPrefix(body, "vps"):
		name = strings.ToUpper(body)
		productRange = "VPS"
	}

	return PlanMeta{
		PlanCode:   planCode,
		BaseCode:   baseCode,
		Name:       name,
		Range:      productRange,
		Generation: InferPlanGeneration(planCode),
		Region:     regionInfo,
	}
}

// ClassifyProductRange normalizes OVH product ranges into KS, RISE, SYS, ADV, SCALE, HGR, or VPS.
func ClassifyProductRange(name, rawRange, planCode string) string {
	up := strings.ToUpper(strings.TrimSpace(name))
	rr := strings.ToLower(strings.TrimSpace(rawRange))
	switch {
	case strings.HasPrefix(up, "KS") || rr == "kimsufi":
		return "KS"
	case strings.HasPrefix(up, "RISE") || rr == "rise":
		return "RISE"
	case strings.HasPrefix(up, "SYS") || rr == "soyoustart":
		return "SYS"
	case strings.HasPrefix(up, "ADV") || strings.HasPrefix(up, "GAME") || rr == "advance" || rr == "game":
		return "ADV"
	case strings.HasPrefix(up, "SCALE") || rr == "scale":
		return "SCALE"
	case strings.HasPrefix(up, "HGR") || strings.HasPrefix(up, "HCI") || strings.HasPrefix(up, "HG-") || strings.HasPrefix(up, "SDS") || rr == "hg" || rr == "high-grade" || rr == "sds":
		return "HGR"
	case strings.HasPrefix(up, "VPS") || rr == "vps":
		return "VPS"
	default:
		return InferPlanFromCode(planCode).Range
	}
}

// FormatCapacityGB formats integer GB into GB or TB (Go/To for French).
func FormatCapacityGB(capGB int, locale string) string {
	unitGB := "GB"
	unitTB := "TB"
	if locale == "fr" {
		unitGB = "Go"
		unitTB = "To"
	}
	if capGB <= 0 {
		return "0 " + unitGB
	}
	if capGB >= 1000 {
		if capGB%1000 == 0 {
			return fmt.Sprintf("%d %s", capGB/1000, unitTB)
		}
		tbStr := strings.TrimRight(strings.TrimRight(fmt.Sprintf("%.2f", float64(capGB)/1000.0), "0"), ".")
		return fmt.Sprintf("%s %s", tbStr, unitTB)
	}
	return fmt.Sprintf("%d %s", capGB, unitGB)
}

// ParseMemoryCode parses an OVH memory code such as "ram-32g-noecc-2133".
func ParseMemoryCode(code string, meta map[string]any) ParsedMemory {
	raw := strings.TrimSpace(code)
	low := strings.ToLower(raw)
	if raw == "" || low == "none" || low == "n/a" {
		return ParsedMemory{Raw: raw}
	}

	var sizeGB, freq int
	var ramType, eccMode string
	if m := reMemoryCode.FindStringSubmatch(raw); len(m) == 5 {
		sizeGB, _ = strconv.Atoi(m[1])
		ramType = strings.ToUpper(m[2])
		eccMode = strings.ToLower(m[3])
		freq, _ = strconv.Atoi(m[4])
	} else if m := reMemoryAny.FindStringSubmatch(raw); len(m) == 2 {
		sizeGB, _ = strconv.Atoi(m[1])
	}

	if meta != nil {
		if sizeGB == 0 {
			sizeGB = intFromAny(meta["size"])
		}
		if ramType == "" {
			if s, ok := meta["ramType"].(string); ok {
				ramType = strings.ToUpper(strings.TrimSpace(s))
			}
		}
		if freq == 0 {
			freq = intFromAny(meta["frequency"])
		}
		if eccMode == "" {
			if eccBool, ok := meta["ecc"].(bool); ok {
				if eccBool {
					eccMode = "ecc"
				} else {
					eccMode = "noecc"
				}
			}
		}
	}

	if ramType == "" && freq > 0 {
		switch {
		case freq <= 1866:
			ramType = "DDR3"
		case freq <= 3200:
			ramType = "DDR4"
		default:
			ramType = "DDR5"
		}
	}

	return ParsedMemory{
		Raw:       raw,
		SizeGB:    sizeGB,
		RAMType:   ramType,
		ECC:       eccMode,
		Frequency: freq,
	}
}

// FormatMemory formats ParsedMemory into localized human-readable text.
func FormatMemory(parsed ParsedMemory, locale string) string {
	if parsed.SizeGB <= 0 {
		return parsed.Raw
	}
	unitGB, unitTB := "GB", "TB"
	if locale == "fr" {
		unitGB, unitTB = "Go", "To"
	}
	sizeStr := fmt.Sprintf("%d %s", parsed.SizeGB, unitGB)
	if parsed.SizeGB >= 1024 && parsed.SizeGB%1024 == 0 {
		sizeStr = fmt.Sprintf("%d %s (%d %s)", parsed.SizeGB, unitGB, parsed.SizeGB/1024, unitTB)
	}
	if parsed.RAMType != "" {
		sizeStr += " " + parsed.RAMType
	}
	parts := []string{sizeStr}
	switch parsed.ECC {
	case "ecc":
		parts = append(parts, "ECC")
	case "on-die-ecc":
		parts = append(parts, "On-Die ECC")
	case "noecc":
		switch locale {
		case "zh-CN":
			parts = append(parts, "非 ECC")
		case "fr":
			parts = append(parts, "Sans ECC")
		default:
			parts = append(parts, "Non-ECC")
		}
	}
	if parsed.Frequency > 0 {
		parts = append(parts, fmt.Sprintf("%d MHz", parsed.Frequency))
	}
	return strings.Join(parts, " · ")
}

// ParseStorageCode parses an OVH storage or systemStorage code into structured disk groups.
func ParseStorageCode(code string, isSystem bool) ParsedStorage {
	raw := strings.TrimSpace(code)
	low := strings.ToLower(raw)
	if raw == "" || low == "none" || low == "n/a" || low == "noraid-0" {
		return ParsedStorage{Raw: raw, None: true, RAID: "none", IsSystem: isSystem}
	}
	if strings.HasSuffix(low, "-system") {
		isSystem = true
		low = strings.TrimSuffix(low, "-system")
	}

	raid := "none"
	rest := low
	switch {
	case strings.HasPrefix(low, "hardraid-"):
		raid = "hard"
		rest = strings.TrimPrefix(low, "hardraid-")
	case strings.HasPrefix(low, "hybridsoftraid-"):
		raid = "hybrid_soft"
		rest = strings.TrimPrefix(low, "hybridsoftraid-")
	case strings.HasPrefix(low, "softraid-"):
		raid = "soft"
		rest = strings.TrimPrefix(low, "softraid-")
	case strings.HasPrefix(low, "noraid-"):
		raid = "none"
		rest = strings.TrimPrefix(low, "noraid-")
	}

	matches := reStorageGroup.FindAllStringSubmatch(rest, -1)
	groups := make([]StorageDiskGroup, 0, len(matches))
	for _, m := range matches {
		count, _ := strconv.Atoi(m[1])
		capGB, _ := strconv.Atoi(m[2])
		hddFlag := m[3]
		media := m[4]
		subIface := m[5]
		pcie := m[6]

		var diskType string
		switch media {
		case "nvme":
			diskType = "NVMe SSD"
		case "ssd":
			switch subIface {
			case "sata":
				diskType = "SATA SSD"
			case "sas":
				diskType = "SAS SSD"
			default:
				diskType = "SSD"
			}
		case "sa", "sata":
			diskType = "SATA HDD"
		case "sas":
			if hddFlag == "hdd" || capGB >= 10000 {
				diskType = "SAS HDD"
			} else {
				diskType = "SAS SSD"
			}
		default:
			diskType = strings.ToUpper(media)
		}

		pcieLabel := ""
		if pcie != "" {
			pcieLabel = strings.ReplaceAll(strings.ToUpper(pcie), "PCIE-GEN", "PCIe Gen")
		}
		groups = append(groups, StorageDiskGroup{
			Count:      count,
			CapacityGB: capGB,
			Type:       diskType,
			PCIe:       pcieLabel,
		})
	}

	return ParsedStorage{
		Raw:      raw,
		None:     false,
		RAID:     raid,
		IsSystem: isSystem,
		Groups:   groups,
	}
}

// FormatStorage formats ParsedStorage into localized human-readable text.
func FormatStorage(parsed ParsedStorage, locale string) string {
	if parsed.None {
		if strings.EqualFold(parsed.Raw, "noraid-0") {
			switch locale {
			case "zh-CN":
				return "无数据盘"
			case "fr":
				return "Aucun disque"
			default:
				return "No Data Drive"
			}
		}
		return ""
	}
	if len(parsed.Groups) == 0 {
		return parsed.Raw
	}

	parts := make([]string, 0, len(parsed.Groups))
	for _, g := range parsed.Groups {
		capStr := FormatCapacityGB(g.CapacityGB, locale)
		t := g.Type
		if g.PCIe != "" {
			t = fmt.Sprintf("%s (%s)", t, g.PCIe)
		}
		parts = append(parts, fmt.Sprintf("%dx %s %s", g.Count, capStr, t))
	}
	joined := strings.Join(parts, " + ")
	if parsed.IsSystem {
		return joined
	}

	var raidLabel string
	switch parsed.RAID {
	case "soft":
		switch locale {
		case "zh-CN":
			raidLabel = "软 RAID"
		case "fr":
			raidLabel = "RAID logiciel"
		default:
			raidLabel = "Soft RAID"
		}
	case "hard":
		switch locale {
		case "zh-CN":
			raidLabel = "硬 RAID"
		case "fr":
			raidLabel = "RAID matériel"
		default:
			raidLabel = "Hard RAID"
		}
	case "hybrid_soft":
		switch locale {
		case "zh-CN":
			raidLabel = "混合软 RAID"
		case "fr":
			raidLabel = "RAID hybride"
		default:
			raidLabel = "Hybrid Soft RAID"
		}
	}
	if raidLabel != "" {
		return joined + " · " + raidLabel
	}
	return joined
}

// ParseGPUCode parses an OVH gpu-* code.
func ParseGPUCode(code string) ParsedGPU {
	raw := strings.TrimSpace(code)
	low := strings.ToLower(raw)
	if raw == "" || low == "none" || low == "n/a" {
		return ParsedGPU{Raw: raw}
	}
	m := reGPUCode.FindStringSubmatch(raw)
	if len(m) != 4 {
		return ParsedGPU{Raw: raw, Count: 1, Model: raw}
	}
	count, _ := strconv.Atoi(m[1])
	slug := strings.ToLower(m[2])
	vram, _ := strconv.Atoi(m[3])

	vendor := ""
	model := strings.ToUpper(slug)
	if strings.HasPrefix(slug, "nvidia-") {
		vendor = "NVIDIA"
		model = strings.ToUpper(strings.TrimPrefix(slug, "nvidia-"))
	} else if strings.HasPrefix(slug, "radeon-") {
		vendor = "AMD Radeon"
		sub := strings.ToUpper(strings.TrimPrefix(slug, "radeon-"))
		model = strings.TrimSpace(reRXModel.ReplaceAllString(sub, "RX $1 $2"))
	}

	return ParsedGPU{
		Raw:    raw,
		Count:  count,
		Vendor: vendor,
		Model:  model,
		VRAMGB: vram,
	}
}

// FormatGPU formats ParsedGPU into readable text.
func FormatGPU(parsed ParsedGPU, locale string) string {
	if parsed.Count <= 0 {
		return parsed.Raw
	}
	unitGB := "GB"
	if locale == "fr" {
		unitGB = "Go"
	}
	name := strings.TrimSpace(parsed.Vendor + " " + parsed.Model)
	if parsed.VRAMGB > 0 {
		return fmt.Sprintf("%dx %s %d %s", parsed.Count, name, parsed.VRAMGB, unitGB)
	}
	return fmt.Sprintf("%dx %s", parsed.Count, name)
}

// FormatCPU formats structured CPUInfo into localized subtitle text.
func FormatCPU(cpu *CPUInfo, locale string) string {
	if cpu == nil {
		return ""
	}
	title := strings.TrimSpace(cpu.Brand + " " + cpu.Model)
	var parts []string
	if title != "" {
		parts = append(parts, title)
	}
	if cpu.Cores > 0 && cpu.Threads > 0 {
		if locale == "zh-CN" {
			parts = append(parts, fmt.Sprintf("%d核/%d线程", cpu.Cores, cpu.Threads))
		} else {
			parts = append(parts, fmt.Sprintf("%dC/%dT", cpu.Cores, cpu.Threads))
		}
	} else if cpu.Cores > 0 {
		if locale == "zh-CN" {
			parts = append(parts, fmt.Sprintf("%d核", cpu.Cores))
		} else {
			parts = append(parts, fmt.Sprintf("%dC", cpu.Cores))
		}
	}
	if cpu.Frequency > 0 && cpu.Boost > 0 && cpu.Boost != cpu.Frequency {
		parts = append(parts, fmt.Sprintf("%s/%s GHz", trimFloat(cpu.Frequency), trimFloat(cpu.Boost)))
	} else if cpu.Frequency > 0 {
		parts = append(parts, fmt.Sprintf("%s GHz", trimFloat(cpu.Frequency)))
	}
	return strings.Join(parts, " · ")
}

// FormatBandwidthCode formats OVH bandwidth/traffic addon codes into readable English strings.
func FormatBandwidthCode(code string) string {
	raw := strings.TrimSpace(code)
	if raw == "" {
		return "N/A"
	}
	if m := reTrafficBW.FindStringSubmatch(raw); len(m) == 4 {
		return fmt.Sprintf("%s Mbps / %s %s Traffic", m[3], m[1], strings.ToUpper(m[2]))
	}
	if m := reTrafficOnly.FindStringSubmatch(raw); len(m) == 3 {
		return fmt.Sprintf("%s %s Traffic", m[1], strings.ToUpper(m[2]))
	}
	if m := reBandwidthNum.FindStringSubmatch(raw); len(m) == 2 {
		bw, _ := strconv.Atoi(m[1])
		if bw >= 1000 {
			return fmt.Sprintf("%s Gbps", trimFloat(float64(bw)/1000.0))
		}
		return fmt.Sprintf("%d Mbps", bw)
	}
	low := strings.ToLower(raw)
	if strings.Contains(low, "unlimited") {
		if m := reFirstDigits.FindStringSubmatch(raw); len(m) == 2 {
			return fmt.Sprintf("%s Mbps / Unlimited", m[1])
		}
		return "Unlimited Traffic"
	}
	if strings.Contains(low, "guarantee") {
		if m := reFirstDigits.FindStringSubmatch(raw); len(m) == 2 {
			return fmt.Sprintf("%s Mbps (Guaranteed)", m[1])
		}
		return "Guaranteed Bandwidth"
	}
	return raw
}

// FormatVRackCode formats OVH vrack-bandwidth-* addon codes into readable English strings.
func FormatVRackCode(code string) string {
	raw := strings.TrimSpace(code)
	if raw == "" {
		return "N/A"
	}
	if m := reVRackBW.FindStringSubmatch(raw); len(m) == 2 {
		bw, _ := strconv.Atoi(m[1])
		if bw >= 1000 {
			return fmt.Sprintf("%s Gbps vRack", trimFloat(float64(bw)/1000.0))
		}
		return fmt.Sprintf("%d Mbps vRack", bw)
	}
	return raw
}

// StandardizeConfig normalizes an addon or availability hardware code for matching.
func StandardizeConfig(configStr string) string {
	if strings.TrimSpace(configStr) == "" {
		return ""
	}
	norm := strings.ToLower(strings.TrimSpace(configStr))
	for _, pat := range standardizePatterns {
		norm = pat.ReplaceAllString(norm, "")
	}
	norm = reStdMemECC.ReplaceAllString(norm, "")
	norm = reStdStorage.ReplaceAllString(norm, "")
	norm = reStdFreq.ReplaceAllString(norm, "")
	return norm
}

func isLicenseOrOSOption(addonCode string) bool {
	low := strings.ToLower(strings.TrimSpace(addonCode))
	for _, skip := range []string{
		"windows-server", "sql-server", "cpanel-license", "plesk-",
		"-license-", "control-panel", "panel", "license", "security",
		"antivirus", "firewall",
	} {
		if strings.Contains(low, skip) {
			return true
		}
	}
	return strings.HasPrefix(low, "os-")
}

func trimFloat(v float64) string {
	if math.Abs(v-math.Round(v)) < 1e-9 {
		return strconv.FormatInt(int64(math.Round(v)), 10)
	}
	return strings.TrimRight(strings.TrimRight(fmt.Sprintf("%.2f", v), "0"), ".")
}

func intFromAny(v any) int {
	switch t := v.(type) {
	case int:
		return t
	case int64:
		return int(t)
	case float64:
		return int(t)
	case string:
		n, _ := strconv.Atoi(strings.TrimSpace(t))
		return n
	default:
		return 0
	}
}
