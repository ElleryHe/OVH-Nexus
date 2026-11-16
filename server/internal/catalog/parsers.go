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
	"apac": {Code: "APAC", ZHCN: "亚太", EN: "APAC", FR: "APAC"},
}

type dcInfoMeta struct {
	Name        string
	Region      string
	CountryCode string
}

var datacenterMetaMap = map[string]dcInfoMeta{
	"gra": {Name: "Gravelines", Region: "France", CountryCode: "FR"},
	"rbx": {Name: "Roubaix", Region: "France", CountryCode: "FR"},
	"sbg": {Name: "Strasbourg", Region: "France", CountryCode: "FR"},
	"par": {Name: "Paris", Region: "France", CountryCode: "FR"},
	"fra": {Name: "Frankfurt", Region: "Germany", CountryCode: "DE"},
	"lon": {Name: "London", Region: "United Kingdom", CountryCode: "GB"},
	"eri": {Name: "Erith", Region: "United Kingdom", CountryCode: "GB"},
	"waw": {Name: "Warsaw", Region: "Poland", CountryCode: "PL"},
	"lim": {Name: "Limassol", Region: "Cyprus", CountryCode: "CY"},
	"bhs": {Name: "Beauharnois", Region: "Canada", CountryCode: "CA"},
	"hil": {Name: "Hillsboro", Region: "United States (West)", CountryCode: "US"},
	"vin": {Name: "Vint Hill", Region: "United States (East)", CountryCode: "US"},
	"sgp": {Name: "Singapore", Region: "Singapore", CountryCode: "SG"},
	"syd": {Name: "Sydney", Region: "Australia", CountryCode: "AU"},
	"mum": {Name: "Mumbai", Region: "India", CountryCode: "IN"},
	"ynm": {Name: "Mumbai", Region: "India", CountryCode: "IN"},
}

var (
	reRegionSuffix  = regexp.MustCompile(`(?i)-(sgp|syd|mum|ynm|apac)$`)
	reVersionSuffix = regexp.MustCompile(`(?i)-v\d+$`)
	reTrailingVer   = regexp.MustCompile(`^(\d{2}[a-z]+\d{2})\d$`)
	rePlanBody      = regexp.MustCompile(`^(\d{2})([a-z0-9-]+)$`)
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

// NormalizeDisplayDatacenter maps OVH internal API datacenter codes (e.g. ynm) to UI display codes (mum).
func NormalizeDisplayDatacenter(dc string) string {
	low := strings.ToLower(strings.TrimSpace(dc))
	if low == "ynm" {
		return "mum"
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

// DatacenterMeta returns (dcName, region, countryCode) for a datacenter code.
func DatacenterMeta(dcCode string) (string, string, string) {
	low := strings.ToLower(strings.TrimSpace(dcCode))
	if len(low) > 3 {
		if m, ok := datacenterMetaMap[low[:3]]; ok {
			return m.Name, m.Region, m.CountryCode
		}
	}
	if m, ok := datacenterMetaMap[low]; ok {
		return m.Name, m.Region, m.CountryCode
	}
	if low == "" {
		return "Unknown", "Unknown", ""
	}
	return strings.ToUpper(low), "Global", ""
}

// InferCartRegion infers the OVH cart item `region` configuration value for a datacenter.
func InferCartRegion(apiDatacenter string) string {
	dc := strings.ToLower(strings.TrimSpace(apiDatacenter))
	for _, p := range []string{"gra", "rbx", "sbg", "eri", "lim", "waw", "par", "fra", "lon"} {
		if strings.HasPrefix(dc, p) {
			return "europe"
		}
	}
	if strings.HasPrefix(dc, "bhs") {
		return "canada"
	}
	if strings.HasPrefix(dc, "vin") || strings.HasPrefix(dc, "hil") {
		return "usa"
	}
	if strings.HasPrefix(dc, "syd") || strings.HasPrefix(dc, "sgp") || strings.HasPrefix(dc, "ynm") || strings.HasPrefix(dc, "mum") {
		return "apac"
	}
	return ""
}

// ParsePlanRegion extracts the base planCode and optional regional suffix tag.
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
	baseCode := reVersionSuffix.ReplaceAllString(code, "")
	return baseCode, regTag
}

// InferPlanFromCode resolves a commercial name and product range deterministically from a planCode.
func InferPlanFromCode(planCode string) PlanMeta {
	baseCode, regionInfo := ParsePlanRegion(planCode)
	clean := reTrailingVer.ReplaceAllString(strings.ToLower(baseCode), "$1")

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
		PlanCode: planCode,
		BaseCode: baseCode,
		Name:     name,
		Range:    productRange,
		Region:   regionInfo,
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
