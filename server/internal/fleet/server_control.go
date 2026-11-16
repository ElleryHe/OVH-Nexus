package fleet

import (
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"

	"ovh-nexus/server/internal/logger"
)

var ticketIDRegex = regexp.MustCompile(`ticketId[:\s]+(\d+)`)

func servicePath(serviceName string) string {
	return "/dedicated/server/" + url.PathEscape(strings.TrimSpace(serviceName))
}

func requireServiceName(w http.ResponseWriter, r *http.Request) (string, bool) {
	srv := strings.TrimSpace(chi.URLParam(r, "serviceName"))
	if srv == "" {
		writeSuccessError(w, http.StatusBadRequest, "serviceName is required")
		return "", false
	}
	return srv, true
}

// HandleListDedicatedServers handles GET /api/server-control/list and /api/server-control/servers.
func (s *Service) HandleListDedicatedServers(w http.ResponseWriter, r *http.Request) {
	client, _, ok := s.requireOVHClient(w, r)
	if !ok {
		return
	}

	ctx := r.Context()
	var serverNames []string
	if err := client.GetWithContext(ctx, "/dedicated/server", &serverNames); err != nil {
		s.logError(logger.SourceOVH, "Failed to list dedicated servers", "error", formatOVHError(err))
		writeSuccessError(w, http.StatusInternalServerError, formatOVHError(err))
		return
	}

	s.logInfo(logger.SourceOVH, "Listed dedicated servers", "count", len(serverNames))

	servers := make([]map[string]any, 0, len(serverNames))
	for _, srv := range serverNames {
		enc := servicePath(srv)
		var serverInfo map[string]any
		if err := client.GetWithContext(ctx, enc, &serverInfo); err != nil {
			s.logError(logger.SourceOVH, "Failed to fetch dedicated server details", "serviceName", srv, "error", formatOVHError(err))
			servers = append(servers, map[string]any{
				"serviceName": srv,
				"name":        srv,
				"error":       formatOVHError(err),
			})
			continue
		}
		var serviceInfo map[string]any
		_ = client.GetWithContext(ctx, enc+"/serviceInfos", &serviceInfo)

		name := asString(serverInfo["name"])
		if name == "" {
			name = srv
		}
		commercialRange := asString(serverInfo["commercialRange"])
		if commercialRange == "" {
			commercialRange = "N/A"
		}
		dc := asString(serverInfo["datacenter"])
		if dc == "" {
			dc = "N/A"
		}
		state := asString(serverInfo["state"])
		if state == "" {
			state = "unknown"
		}
		ipAddr := asString(serverInfo["ip"])
		if ipAddr == "" {
			ipAddr = "N/A"
		}
		osName := asString(serverInfo["os"])
		if osName == "" {
			osName = "N/A"
		}
		status := asString(serviceInfo["status"])
		if status == "" {
			status = "unknown"
		}
		renewObj := asMap(serviceInfo["renew"])

		servers = append(servers, map[string]any{
			"serviceName":     srv,
			"name":            name,
			"commercialRange": commercialRange,
			"datacenter":      dc,
			"state":           state,
			"monitoring":      asBool(serverInfo["monitoring"]),
			"reverse":         asString(serverInfo["reverse"]),
			"ip":              ipAddr,
			"os":              osName,
			"bootId":          serverInfo["bootId"],
			"professionalUse": asBool(serverInfo["professionalUse"]),
			"status":          status,
			"renewalType":     asBool(renewObj["automatic"]),
		})
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"success": true,
		"servers": servers,
		"total":   len(servers),
	})
}

// HandleGetDedicatedServerDetail handles GET /api/server-control/{serviceName}.
func (s *Service) HandleGetDedicatedServerDetail(w http.ResponseWriter, r *http.Request) {
	client, _, ok := s.requireOVHClient(w, r)
	if !ok {
		return
	}
	srv, ok := requireServiceName(w, r)
	if !ok {
		return
	}

	ctx := r.Context()
	enc := servicePath(srv)
	var serverInfo map[string]any
	if err := client.GetWithContext(ctx, enc, &serverInfo); err != nil {
		status := http.StatusInternalServerError
		if isOVHNotFound(err) {
			status = http.StatusNotFound
		}
		writeSuccessError(w, status, formatOVHError(err))
		return
	}
	var serviceInfo map[string]any
	_ = client.GetWithContext(ctx, enc+"/serviceInfos", &serviceInfo)

	serverInfo["serviceName"] = srv
	if len(serviceInfo) > 0 {
		serverInfo["serviceInfo"] = serviceInfo
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"success": true,
		"server":  serverInfo,
	})
}

// HandleRebootServer handles POST /api/server-control/{serviceName}/reboot.
func (s *Service) HandleRebootServer(w http.ResponseWriter, r *http.Request) {
	client, _, ok := s.requireOVHClient(w, r)
	if !ok {
		return
	}
	srv, ok := requireServiceName(w, r)
	if !ok {
		return
	}

	var res map[string]any
	if err := client.PostWithContext(r.Context(), servicePath(srv)+"/reboot", nil, &res); err != nil {
		s.logError(logger.SourceOVH, "Failed to reboot dedicated server", "serviceName", srv, "error", formatOVHError(err))
		writeSuccessError(w, http.StatusInternalServerError, formatOVHError(err))
		return
	}

	s.logInfo(logger.SourceOVH, "Initiated dedicated server reboot", "serviceName", srv)
	writeJSON(w, http.StatusOK, map[string]any{
		"success": true,
		"message": fmt.Sprintf("Reboot request sent for server %s", srv),
		"taskId":  res["taskId"],
	})
}

// HandleGetOSTemplates handles GET /api/server-control/{serviceName}/templates.
func (s *Service) HandleGetOSTemplates(w http.ResponseWriter, r *http.Request) {
	client, _, ok := s.requireOVHClient(w, r)
	if !ok {
		return
	}
	srv, ok := requireServiceName(w, r)
	if !ok {
		return
	}

	ctx := r.Context()
	var compat struct {
		OVH []string `json:"ovh"`
	}
	if err := client.GetWithContext(ctx, servicePath(srv)+"/install/compatibleTemplates", &compat); err != nil {
		s.logError(logger.SourceOVH, "Failed to fetch compatible OS templates", "serviceName", srv, "error", formatOVHError(err))
		writeSuccessError(w, http.StatusInternalServerError, formatOVHError(err))
		return
	}

	templates := make([]map[string]any, 0, len(compat.OVH))
	for _, tplName := range compat.OVH {
		var detail map[string]any
		if err := client.GetWithContext(ctx, "/dedicated/installationTemplate/"+url.PathEscape(tplName), &detail); err != nil {
			templates = append(templates, map[string]any{
				"templateName": tplName,
				"distribution": tplName,
				"family":       "unknown",
				"bitFormat":    64,
			})
			continue
		}
		dist := asString(detail["distribution"])
		if dist == "" {
			dist = "N/A"
		}
		fam := asString(detail["family"])
		if fam == "" {
			fam = "N/A"
		}
		bitFmt := detail["bitFormat"]
		if bitFmt == nil {
			bitFmt = 64
		}
		templates = append(templates, map[string]any{
			"templateName": tplName,
			"distribution": dist,
			"family":       fam,
			"description":  asString(detail["description"]),
			"bitFormat":    bitFmt,
		})
	}

	priorityOrder := []string{"debian", "ubuntu", "centos", "rocky", "almalinux", "windows"}
	priorityOf := func(dist string) int {
		low := strings.ToLower(dist)
		for idx, p := range priorityOrder {
			if strings.Contains(low, p) {
				return idx
			}
		}
		return len(priorityOrder)
	}
	sort.Slice(templates, func(i, j int) bool {
		pi := priorityOf(asString(templates[i]["distribution"]))
		pj := priorityOf(asString(templates[j]["distribution"]))
		if pi != pj {
			return pi < pj
		}
		return asString(templates[i]["templateName"]) < asString(templates[j]["templateName"])
	})

	writeJSON(w, http.StatusOK, map[string]any{
		"success":   true,
		"templates": templates,
		"total":     len(templates),
	})
}

// HandleGetPartitionSchemes handles GET /api/server-control/{serviceName}/partition-schemes.
func (s *Service) HandleGetPartitionSchemes(w http.ResponseWriter, r *http.Request) {
	client, _, ok := s.requireOVHClient(w, r)
	if !ok {
		return
	}
	if _, ok := requireServiceName(w, r); !ok {
		return
	}

	tplName := strings.TrimSpace(r.URL.Query().Get("templateName"))
	if tplName == "" {
		writeSuccessError(w, http.StatusBadRequest, "templateName query parameter is required")
		return
	}

	ctx := r.Context()
	encTpl := url.PathEscape(tplName)
	var schemes []string
	if err := client.GetWithContext(ctx, "/dedicated/installationTemplate/"+encTpl+"/partitionScheme", &schemes); err != nil {
		s.logError(logger.SourceOVH, "Failed to fetch partition schemes", "templateName", tplName, "error", formatOVHError(err))
		writeSuccessError(w, http.StatusInternalServerError, formatOVHError(err))
		return
	}

	schemeDetails := make([]map[string]any, 0, len(schemes))
	for _, schemeName := range schemes {
		encScheme := url.PathEscape(schemeName)
		var schemeInfo map[string]any
		if err := client.GetWithContext(ctx, "/dedicated/installationTemplate/"+encTpl+"/partitionScheme/"+encScheme, &schemeInfo); err != nil {
			schemeDetails = append(schemeDetails, map[string]any{
				"name":       schemeName,
				"priority":   0,
				"partitions": []any{},
			})
			continue
		}

		var partNames []string
		_ = client.GetWithContext(ctx, "/dedicated/installationTemplate/"+encTpl+"/partitionScheme/"+encScheme+"/partition", &partNames)

		parts := make([]map[string]any, 0, len(partNames))
		for _, partName := range partNames {
			var pInfo map[string]any
			if err := client.GetWithContext(ctx, "/dedicated/installationTemplate/"+encTpl+"/partitionScheme/"+encScheme+"/partition/"+url.PathEscape(partName), &pInfo); err != nil {
				continue
			}
			partType := asString(pInfo["type"])
			if partType == "" {
				partType = "primary"
			}
			parts = append(parts, map[string]any{
				"mountpoint": partName,
				"filesystem": asString(pInfo["filesystem"]),
				"size":       asInt(pInfo["size"], 0),
				"order":      asInt(pInfo["order"], 0),
				"raid":       pInfo["raid"],
				"type":       partType,
			})
		}

		sort.Slice(parts, func(i, j int) bool {
			return asInt(parts[i]["order"], 0) < asInt(parts[j]["order"], 0)
		})

		schemeDetails = append(schemeDetails, map[string]any{
			"name":       schemeName,
			"priority":   asInt(schemeInfo["priority"], 0),
			"partitions": parts,
		})
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"success": true,
		"schemes": schemeDetails,
	})
}

// HandleGetHardwareDiskInfo handles GET /api/server-control/{serviceName}/hardware-disk-info.
func (s *Service) HandleGetHardwareDiskInfo(w http.ResponseWriter, r *http.Request) {
	client, _, ok := s.requireOVHClient(w, r)
	if !ok {
		return
	}
	srv, ok := requireServiceName(w, r)
	if !ok {
		return
	}

	var hardware map[string]any
	if err := client.GetWithContext(r.Context(), servicePath(srv)+"/specifications/hardware", &hardware); err != nil {
		s.logError(logger.SourceOVH, "Failed to fetch hardware disk info", "serviceName", srv, "error", formatOVHError(err))
		writeSuccessError(w, http.StatusInternalServerError, formatOVHError(err))
		return
	}

	diskGroups := make(map[string]any)
	if rawGroups, ok := hardware["diskGroups"].([]any); ok {
		for _, item := range rawGroups {
			grp := asMap(item)
			gid := asInt(grp["diskGroupId"], 0)
			numDisks := asInt(grp["numberOfDisks"], 0)
			diskSize := asMap(grp["diskSize"])
			sizeVal := asInt(diskSize["value"], 0)
			sizeUnit := asString(diskSize["unit"])
			if sizeUnit == "" {
				sizeUnit = "GB"
			}

			disks := make([]map[string]any, 0, numDisks)
			for i := 0; i < numDisks; i++ {
				disks = append(disks, map[string]any{
					"capacity": sizeVal,
					"unit":     sizeUnit,
					"number":   i + 1,
					"diskType": grp["diskType"],
				})
			}

			diskGroups[strconv.Itoa(gid)] = map[string]any{
				"id":             gid,
				"diskType":       grp["diskType"],
				"description":    grp["description"],
				"raidController": grp["raidController"],
				"disks":          disks,
			}
		}
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"success":    true,
		"diskGroups": diskGroups,
		"hardware":   hardware,
	})
}

// HandleGetHardwareRAIDProfiles handles GET /api/server-control/{serviceName}/hardware-raid-profiles.
func (s *Service) HandleGetHardwareRAIDProfiles(w http.ResponseWriter, r *http.Request) {
	client, _, ok := s.requireOVHClient(w, r)
	if !ok {
		return
	}
	srv, ok := requireServiceName(w, r)
	if !ok {
		return
	}

	var profiles any
	if err := client.GetWithContext(r.Context(), servicePath(srv)+"/install/hardwareRaidProfile", &profiles); err != nil {
		if strings.Contains(strings.ToLower(err.Error()), "not supported") {
			writeJSON(w, http.StatusOK, map[string]any{
				"success":   true,
				"profiles":  []any{},
				"supported": false,
				"message":   "Hardware RAID is not supported on this server",
			})
			return
		}
		s.logError(logger.SourceOVH, "Failed to fetch hardware RAID profiles", "serviceName", srv, "error", formatOVHError(err))
		writeSuccessError(w, http.StatusInternalServerError, formatOVHError(err))
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"success":   true,
		"profiles":  profiles,
		"supported": true,
	})
}

// HandleInstallOS handles POST /api/server-control/{serviceName}/install.
func (s *Service) HandleInstallOS(w http.ResponseWriter, r *http.Request) {
	client, _, ok := s.requireOVHClient(w, r)
	if !ok {
		return
	}
	srv, ok := requireServiceName(w, r)
	if !ok {
		return
	}

	var body struct {
		TemplateName        string `json:"templateName"`
		CustomHostname      string `json:"customHostname"`
		SSHKey              string `json:"sshKey"`
		SSHKeyName          string `json:"sshKeyName"`
		UseProxmox9ZFS      bool   `json:"useProxmox9Zfs"`
		ZFSRaidLevel        *int   `json:"zfsRaidLevel"`
		ZFSVzSize           *int   `json:"zfsVzSize"`
		StorageConfig       any    `json:"storageConfig"`
		PartitionSchemeName string `json:"partitionSchemeName"`
	}
	if err := decodeJSONBody(r, &body); err != nil {
		writeSuccessError(w, http.StatusBadRequest, "Invalid JSON payload")
		return
	}

	tplName := strings.TrimSpace(body.TemplateName)
	if tplName == "" {
		writeSuccessError(w, http.StatusBadRequest, "templateName is required")
		return
	}

	ctx := r.Context()
	installParams := map[string]any{
		"operatingSystem": tplName,
	}

	hostname := strings.TrimSpace(body.CustomHostname)
	if hostname != "" {
		installParams["customHostname"] = hostname
	}

	sshKeyContent := strings.TrimSpace(body.SSHKey)
	sshKeyName := strings.TrimSpace(body.SSHKeyName)
	if sshKeyName != "" && sshKeyContent == "" {
		var keyObj map[string]any
		if err := client.GetWithContext(ctx, "/me/sshKey/"+url.PathEscape(sshKeyName), &keyObj); err != nil {
			s.logWarn(logger.SourceOVH, "Failed to read SSH key for server install", "keyName", sshKeyName, "error", formatOVHError(err))
		} else {
			sshKeyContent = strings.TrimSpace(asString(keyObj["key"]))
		}
	}
	if sshKeyContent != "" || sshKeyName != "" {
		customizations := map[string]any{}
		if sshKeyContent != "" {
			customizations["sshKey"] = sshKeyContent
		}
		if hostname != "" {
			customizations["hostname"] = hostname
		}
		if len(customizations) > 0 {
			installParams["customizations"] = customizations
		}
	}

	if body.UseProxmox9ZFS {
		raidLevel := 1
		if body.ZFSRaidLevel != nil {
			raidLevel = *body.ZFSRaidLevel
		}
		vzSizeMB := 102400
		if body.ZFSVzSize != nil && *body.ZFSVzSize > 0 {
			vzSizeMB = *body.ZFSVzSize
		}

		totalCapacityGB := 480
		if raidLevel == 0 {
			totalCapacityGB = 960
		}
		var hw map[string]any
		if err := client.GetWithContext(ctx, servicePath(srv)+"/specifications/hardware", &hw); err == nil {
			if groups, ok := hw["diskGroups"].([]any); ok && len(groups) > 0 {
				first := asMap(groups[0])
				diskCount := asInt(first["numberOfDisks"], 2)
				singleGB := asInt(asMap(first["diskSize"])["value"], 480)
				if raidLevel == 0 {
					totalCapacityGB = singleGB * diskCount
				} else {
					totalCapacityGB = singleGB
				}
			}
		}

		usableMB := int(float64(totalCapacityGB) * 1024 * 0.92)
		bootSwapMB := 1024 + 8192
		rootSizeMB := usableMB - bootSwapMB - vzSizeMB
		if rootSizeMB < 10240 {
			rootSizeMB = 10240
		}

		installParams["storage"] = []map[string]any{
			{
				"diskGroupId": 0,
				"partitioning": map[string]any{
					"layout": []map[string]any{
						{
							"fileSystem": "ext4",
							"mountPoint": "/boot",
							"raidLevel":  raidLevel,
							"size":       1024,
						},
						{
							"fileSystem": "swap",
							"mountPoint": "swap",
							"raidLevel":  1,
							"size":       8192,
						},
						{
							"fileSystem": "zfs",
							"mountPoint": "/",
							"raidLevel":  raidLevel,
							"size":       rootSizeMB,
							"extras":     map[string]any{"zp": map[string]any{"name": "rpool"}},
						},
						{
							"fileSystem": "zfs",
							"mountPoint": "/var/lib/vz",
							"raidLevel":  raidLevel,
							"size":       0,
							"extras":     map[string]any{"zp": map[string]any{"name": "rpool"}},
						},
					},
				},
			},
		}
	} else if body.StorageConfig != nil {
		installParams["storage"] = body.StorageConfig
	} else if strings.TrimSpace(body.PartitionSchemeName) != "" {
		installParams["partitionSchemeName"] = strings.TrimSpace(body.PartitionSchemeName)
	}

	var res map[string]any
	if err := client.PostWithContext(ctx, servicePath(srv)+"/reinstall", installParams, &res); err != nil {
		s.logError(logger.SourceOVH, "Failed to initiate OS reinstall", "serviceName", srv, "template", tplName, "error", formatOVHError(err))
		writeSuccessError(w, http.StatusInternalServerError, formatOVHError(err))
		return
	}

	s.logWarn(logger.SourceOVH, "Initiated dedicated server OS reinstall", "serviceName", srv, "template", tplName)
	writeJSON(w, http.StatusOK, map[string]any{
		"success": true,
		"message": fmt.Sprintf("OS reinstall initiated for server %s", srv),
		"taskId":  res["taskId"],
	})
}

// HandleGetInstallStatus handles GET /api/server-control/{serviceName}/install/status.
func (s *Service) HandleGetInstallStatus(w http.ResponseWriter, r *http.Request) {
	client, _, ok := s.requireOVHClient(w, r)
	if !ok {
		return
	}
	srv, ok := requireServiceName(w, r)
	if !ok {
		return
	}

	var raw struct {
		ElapsedTime int `json:"elapsedTime"`
		Progress    []struct {
			Comment string `json:"comment"`
			Status  string `json:"status"`
			Error   string `json:"error"`
		} `json:"progress"`
	}
	if err := client.GetWithContext(r.Context(), servicePath(srv)+"/install/status", &raw); err != nil {
		if isOVHNotFound(err) {
			writeJSON(w, http.StatusOK, map[string]any{
				"success":         true,
				"hasInstallation": false,
				"message":         "No OS installation is currently in progress",
			})
			return
		}
		s.logError(logger.SourceOVH, "Failed to fetch server install status", "serviceName", srv, "error", formatOVHError(err))
		writeSuccessError(w, http.StatusInternalServerError, formatOVHError(err))
		return
	}

	totalSteps := len(raw.Progress)
	completedSteps := 0
	hasError := false
	steps := make([]map[string]any, 0, totalSteps)
	for _, st := range raw.Progress {
		if st.Status == "done" {
			completedSteps++
		}
		if st.Status == "error" {
			hasError = true
		}
		steps = append(steps, map[string]any{
			"comment":         st.Comment,
			"commentOriginal": st.Comment,
			"status":          st.Status,
			"error":           st.Error,
		})
	}

	progressPct := 0
	if totalSteps > 0 {
		progressPct = (completedSteps * 100) / totalSteps
	}
	allDone := totalSteps > 0 && completedSteps == totalSteps

	writeJSON(w, http.StatusOK, map[string]any{
		"success": true,
		"status": map[string]any{
			"elapsedTime":        raw.ElapsedTime,
			"progressPercentage": progressPct,
			"totalSteps":         totalSteps,
			"completedSteps":     completedSteps,
			"hasError":           hasError,
			"allDone":            allDone,
			"steps":              steps,
		},
	})
}

// HandleGetServerTasks handles GET /api/server-control/{serviceName}/tasks.
func (s *Service) HandleGetServerTasks(w http.ResponseWriter, r *http.Request) {
	client, _, ok := s.requireOVHClient(w, r)
	if !ok {
		return
	}
	srv, ok := requireServiceName(w, r)
	if !ok {
		return
	}

	ctx := r.Context()
	enc := servicePath(srv)
	var taskIDs []int64
	if err := client.GetWithContext(ctx, enc+"/task", &taskIDs); err != nil {
		s.logError(logger.SourceOVH, "Failed to list server tasks", "serviceName", srv, "error", formatOVHError(err))
		writeSuccessError(w, http.StatusInternalServerError, formatOVHError(err))
		return
	}

	start := 0
	if len(taskIDs) > 10 {
		start = len(taskIDs) - 10
	}
	recentIDs := taskIDs[start:]
	tasks := make([]map[string]any, 0, len(recentIDs))
	for _, tid := range recentIDs {
		var detail map[string]any
		if err := client.GetWithContext(ctx, fmt.Sprintf("%s/task/%d", enc, tid), &detail); err != nil {
			continue
		}
		fn := asString(detail["function"])
		if fn == "" {
			fn = "N/A"
		}
		st := asString(detail["status"])
		if st == "" {
			st = "unknown"
		}
		tasks = append(tasks, map[string]any{
			"taskId":    tid,
			"function":  fn,
			"status":    st,
			"comment":   asString(detail["comment"]),
			"startDate": asString(detail["startDate"]),
			"doneDate":  asString(detail["doneDate"]),
		})
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"success": true,
		"tasks":   tasks,
		"total":   len(tasks),
	})
}

// HandleGetTaskTimeslots handles GET /api/server-control/{serviceName}/tasks/{taskId}/available-timeslots.
func (s *Service) HandleGetTaskTimeslots(w http.ResponseWriter, r *http.Request) {
	client, _, ok := s.requireOVHClient(w, r)
	if !ok {
		return
	}
	srv, ok := requireServiceName(w, r)
	if !ok {
		return
	}

	taskID, err := strconv.ParseInt(chi.URLParam(r, "taskId"), 10, 64)
	if err != nil {
		writeSuccessError(w, http.StatusBadRequest, "Invalid taskId")
		return
	}

	pStart := strings.TrimSpace(r.URL.Query().Get("periodStart"))
	pEnd := strings.TrimSpace(r.URL.Query().Get("periodEnd"))
	if pStart == "" || pEnd == "" {
		writeSuccessError(w, http.StatusBadRequest, "periodStart and periodEnd query parameters are required")
		return
	}

	q := url.Values{}
	q.Set("periodStart", pStart)
	q.Set("periodEnd", pEnd)

	var slots []any
	if err := client.GetWithContext(r.Context(), fmt.Sprintf("%s/task/%d/availableTimeslots?%s", servicePath(srv), taskID, q.Encode()), &slots); err != nil {
		low := strings.ToLower(err.Error())
		if strings.Contains(low, "no schedule needed") {
			writeJSON(w, http.StatusOK, map[string]any{
				"success":             true,
				"timeslots":           []any{},
				"scheduleNotRequired": true,
				"message":             "No schedule needed for this task",
			})
			return
		}
		if isOVHNotFound(err) {
			writeSuccessError(w, http.StatusNotFound, "Task or server not found")
			return
		}
		writeSuccessError(w, http.StatusBadGateway, formatOVHError(err))
		return
	}
	if slots == nil {
		slots = []any{}
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"success":   true,
		"timeslots": slots,
	})
}

// HandleScheduleTask handles POST /api/server-control/{serviceName}/tasks/{taskId}/schedule.
func (s *Service) HandleScheduleTask(w http.ResponseWriter, r *http.Request) {
	client, _, ok := s.requireOVHClient(w, r)
	if !ok {
		return
	}
	srv, ok := requireServiceName(w, r)
	if !ok {
		return
	}

	taskID, err := strconv.ParseInt(chi.URLParam(r, "taskId"), 10, 64)
	if err != nil {
		writeSuccessError(w, http.StatusBadRequest, "Invalid taskId")
		return
	}

	var body struct {
		StartDate string `json:"startDate"`
		EndDate   string `json:"endDate"`
	}
	if err := decodeJSONBody(r, &body); err != nil {
		writeSuccessError(w, http.StatusBadRequest, "Invalid JSON payload")
		return
	}
	if strings.TrimSpace(body.StartDate) == "" || strings.TrimSpace(body.EndDate) == "" {
		writeSuccessError(w, http.StatusBadRequest, "startDate and endDate are required")
		return
	}

	var result any
	if err := client.PostWithContext(r.Context(), fmt.Sprintf("%s/task/%d/schedule", servicePath(srv), taskID), map[string]string{
		"startDate": body.StartDate,
		"endDate":   body.EndDate,
	}, &result); err != nil {
		low := strings.ToLower(err.Error())
		if strings.Contains(low, "no schedule needed") {
			writeSuccessError(w, http.StatusBadRequest, "This task does not require or support scheduling")
			return
		}
		if isOVHNotFound(err) {
			writeSuccessError(w, http.StatusNotFound, "Task or server not found")
			return
		}
		writeSuccessError(w, http.StatusBadGateway, formatOVHError(err))
		return
	}

	s.logInfo(logger.SourceOVH, "Scheduled dedicated server task", "serviceName", srv, "taskId", taskID)
	writeJSON(w, http.StatusOK, map[string]any{
		"success": true,
		"result":  result,
	})
}

// HandleGetBootConfig handles GET /api/server-control/{serviceName}/boot.
func (s *Service) HandleGetBootConfig(w http.ResponseWriter, r *http.Request) {
	client, _, ok := s.requireOVHClient(w, r)
	if !ok {
		return
	}
	srv, ok := requireServiceName(w, r)
	if !ok {
		return
	}

	ctx := r.Context()
	enc := servicePath(srv)
	var serverInfo map[string]any
	if err := client.GetWithContext(ctx, enc, &serverInfo); err != nil {
		writeSuccessError(w, http.StatusInternalServerError, formatOVHError(err))
		return
	}
	curBootID := asInt(serverInfo["bootId"], 0)

	var bootIDs []int64
	if err := client.GetWithContext(ctx, enc+"/boot", &bootIDs); err != nil {
		writeSuccessError(w, http.StatusInternalServerError, formatOVHError(err))
		return
	}

	boots := make([]map[string]any, 0, len(bootIDs))
	for _, bid := range bootIDs {
		var detail map[string]any
		if err := client.GetWithContext(ctx, fmt.Sprintf("%s/boot/%d", enc, bid), &detail); err != nil {
			continue
		}
		bootType := asString(detail["bootType"])
		if bootType == "" {
			bootType = "N/A"
		}
		boots = append(boots, map[string]any{
			"id":          bid,
			"bootType":    bootType,
			"description": asString(detail["description"]),
			"kernel":      asString(detail["kernel"]),
			"isCurrent":   int(bid) == curBootID,
		})
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"success":       true,
		"currentBootId": serverInfo["bootId"],
		"boots":         boots,
	})
}

// HandleSetBootConfig handles PUT /api/server-control/{serviceName}/boot/{bootId}.
func (s *Service) HandleSetBootConfig(w http.ResponseWriter, r *http.Request) {
	client, _, ok := s.requireOVHClient(w, r)
	if !ok {
		return
	}
	srv, ok := requireServiceName(w, r)
	if !ok {
		return
	}

	bootID, err := strconv.ParseInt(chi.URLParam(r, "bootId"), 10, 64)
	if err != nil {
		writeSuccessError(w, http.StatusBadRequest, "Invalid bootId")
		return
	}

	if err := client.PutWithContext(r.Context(), servicePath(srv), map[string]int64{"bootId": bootID}, nil); err != nil {
		s.logError(logger.SourceOVH, "Failed to update server boot config", "serviceName", srv, "bootId", bootID, "error", formatOVHError(err))
		writeSuccessError(w, http.StatusInternalServerError, formatOVHError(err))
		return
	}

	s.logInfo(logger.SourceOVH, "Updated server boot configuration", "serviceName", srv, "bootId", bootID)
	writeJSON(w, http.StatusOK, map[string]any{
		"success": true,
		"message": "Boot mode updated; reboot required to take effect",
	})
}

// HandleGetBootModes handles GET /api/server-control/{serviceName}/boot-mode.
func (s *Service) HandleGetBootModes(w http.ResponseWriter, r *http.Request) {
	client, _, ok := s.requireOVHClient(w, r)
	if !ok {
		return
	}
	srv, ok := requireServiceName(w, r)
	if !ok {
		return
	}

	ctx := r.Context()
	enc := servicePath(srv)
	var serverInfo map[string]any
	if err := client.GetWithContext(ctx, enc, &serverInfo); err != nil {
		writeSuccessError(w, http.StatusInternalServerError, formatOVHError(err))
		return
	}
	curBootID := asInt(serverInfo["bootId"], 0)

	var bootIDs []int64
	if err := client.GetWithContext(ctx, enc+"/boot", &bootIDs); err != nil {
		writeSuccessError(w, http.StatusInternalServerError, formatOVHError(err))
		return
	}

	modes := make([]map[string]any, 0, len(bootIDs))
	for _, bid := range bootIDs {
		var detail map[string]any
		if err := client.GetWithContext(ctx, fmt.Sprintf("%s/boot/%d", enc, bid), &detail); err != nil {
			continue
		}
		modes = append(modes, map[string]any{
			"id":          bid,
			"bootType":    detail["bootType"],
			"description": detail["description"],
			"kernel":      detail["kernel"],
			"active":      int(bid) == curBootID,
		})
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"success":       true,
		"currentBootId": serverInfo["bootId"],
		"bootModes":     modes,
	})
}

// HandleSetBootMode handles PUT /api/server-control/{serviceName}/boot-mode.
func (s *Service) HandleSetBootMode(w http.ResponseWriter, r *http.Request) {
	client, _, ok := s.requireOVHClient(w, r)
	if !ok {
		return
	}
	srv, ok := requireServiceName(w, r)
	if !ok {
		return
	}

	var body struct {
		BootID *int64 `json:"bootId"`
	}
	if err := decodeJSONBody(r, &body); err != nil || body.BootID == nil {
		writeSuccessError(w, http.StatusBadRequest, "bootId is required")
		return
	}

	if err := client.PutWithContext(r.Context(), servicePath(srv), map[string]int64{"bootId": *body.BootID}, nil); err != nil {
		s.logError(logger.SourceOVH, "Failed to switch server boot mode", "serviceName", srv, "bootId", *body.BootID, "error", formatOVHError(err))
		writeSuccessError(w, http.StatusInternalServerError, formatOVHError(err))
		return
	}

	s.logInfo(logger.SourceOVH, "Switched server boot mode", "serviceName", srv, "bootId", *body.BootID)
	writeJSON(w, http.StatusOK, map[string]any{
		"success": true,
		"message": "Boot mode updated; reboot required to take effect",
		"bootId":  *body.BootID,
	})
}

// HandleGetMonitoring handles GET /api/server-control/{serviceName}/monitoring.
func (s *Service) HandleGetMonitoring(w http.ResponseWriter, r *http.Request) {
	client, _, ok := s.requireOVHClient(w, r)
	if !ok {
		return
	}
	srv, ok := requireServiceName(w, r)
	if !ok {
		return
	}

	var serverInfo map[string]any
	if err := client.GetWithContext(r.Context(), servicePath(srv), &serverInfo); err != nil {
		writeSuccessError(w, http.StatusInternalServerError, formatOVHError(err))
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"success":    true,
		"monitoring": asBool(serverInfo["monitoring"]),
	})
}

// HandleSetMonitoring handles PUT /api/server-control/{serviceName}/monitoring.
func (s *Service) HandleSetMonitoring(w http.ResponseWriter, r *http.Request) {
	client, _, ok := s.requireOVHClient(w, r)
	if !ok {
		return
	}
	srv, ok := requireServiceName(w, r)
	if !ok {
		return
	}

	var body struct {
		Enabled bool `json:"enabled"`
	}
	if err := decodeJSONBody(r, &body); err != nil {
		writeSuccessError(w, http.StatusBadRequest, "Invalid JSON payload")
		return
	}

	if err := client.PutWithContext(r.Context(), servicePath(srv), map[string]bool{"monitoring": body.Enabled}, nil); err != nil {
		s.logError(logger.SourceOVH, "Failed to update monitoring state", "serviceName", srv, "error", formatOVHError(err))
		writeSuccessError(w, http.StatusInternalServerError, formatOVHError(err))
		return
	}

	stateWord := "disabled"
	if body.Enabled {
		stateWord = "enabled"
	}
	s.logInfo(logger.SourceOVH, "Updated dedicated server monitoring", "serviceName", srv, "enabled", body.Enabled)
	writeJSON(w, http.StatusOK, map[string]any{
		"success": true,
		"message": "Monitoring " + stateWord,
	})
}

// HandleGetHardware handles GET /api/server-control/{serviceName}/hardware.
func (s *Service) HandleGetHardware(w http.ResponseWriter, r *http.Request) {
	client, _, ok := s.requireOVHClient(w, r)
	if !ok {
		return
	}
	srv, ok := requireServiceName(w, r)
	if !ok {
		return
	}

	var hw map[string]any
	if err := client.GetWithContext(r.Context(), servicePath(srv)+"/specifications/hardware", &hw); err != nil {
		writeSuccessError(w, http.StatusInternalServerError, formatOVHError(err))
		return
	}

	orDefault := func(v any, def string) string {
		if s := asString(v); s != "" {
			return s
		}
		return def
	}
	orEmptySlice := func(v any) any {
		if v == nil {
			return []any{}
		}
		return v
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"success": true,
		"hardware": map[string]any{
			"bootMode":                orDefault(hw["bootMode"], "N/A"),
			"coresPerProcessor":       asInt(hw["coresPerProcessor"], 0),
			"threadsPerProcessor":     asInt(hw["threadsPerProcessor"], 0),
			"numberOfProcessors":      asInt(hw["numberOfProcessors"], 0),
			"processorName":           orDefault(hw["processorName"], "N/A"),
			"processorArchitecture":   orDefault(hw["processorArchitecture"], "N/A"),
			"memorySize":              asMap(hw["memorySize"]),
			"motherboard":             orDefault(hw["motherboard"], "N/A"),
			"formFactor":              orDefault(hw["formFactor"], "N/A"),
			"description":             asString(hw["description"]),
			"diskGroups":              orEmptySlice(hw["diskGroups"]),
			"expansionCards":          orEmptySlice(hw["expansionCards"]),
			"usbKeys":                 orEmptySlice(hw["usbKeys"]),
			"defaultHardwareRaidSize": asMap(hw["defaultHardwareRaidSize"]),
			"defaultHardwareRaidType": orDefault(hw["defaultHardwareRaidType"], "N/A"),
		},
	})
}

// HandleHardwareReplace handles POST /api/server-control/{serviceName}/hardware/replace.
func (s *Service) HandleHardwareReplace(w http.ResponseWriter, r *http.Request) {
	client, _, ok := s.requireOVHClient(w, r)
	if !ok {
		return
	}
	srv, ok := requireServiceName(w, r)
	if !ok {
		return
	}

	var body struct {
		ComponentType string `json:"componentType"`
		Comment       string `json:"comment"`
		Details       string `json:"details"`
	}
	if err := decodeJSONBody(r, &body); err != nil {
		writeSuccessError(w, http.StatusBadRequest, "Invalid JSON payload")
		return
	}
	comp := strings.TrimSpace(body.ComponentType)
	if comp == "" {
		writeSuccessError(w, http.StatusBadRequest, "componentType is required")
		return
	}

	ctx := r.Context()
	enc := servicePath(srv)
	var apiPath string
	var payload map[string]any
	comment := strings.TrimSpace(body.Comment)
	details := strings.TrimSpace(body.Details)

	switch comp {
	case "hardDiskDrive":
		apiPath = enc + "/support/replace/hardDiskDrive"
		if comment == "" {
			comment = "Request hard disk drive replacement - faulty disk detected"
		}
		payload = map[string]any{
			"comment": comment,
			"disks":   []string{},
			"inverse": true,
		}
	case "memory":
		apiPath = enc + "/support/replace/memory"
		if comment == "" {
			comment = "Request memory module replacement - hardware failure detected"
		}
		if details == "" {
			details = "Memory module failure"
		}
		payload = map[string]any{
			"comment":          comment,
			"details":          details,
			"slotsDescription": "",
		}
	case "cooling":
		apiPath = enc + "/support/replace/cooling"
		if comment == "" {
			comment = "Request cooling system replacement - fan failure or overheating"
		}
		if details == "" {
			details = "Cooling system failure"
		}
		payload = map[string]any{
			"comment": comment,
			"details": details,
		}
	default:
		writeSuccessError(w, http.StatusBadRequest, "Unsupported componentType: "+comp)
		return
	}

	var result any
	if err := client.PostWithContext(ctx, apiPath, payload, &result); err != nil {
		errMsg := formatOVHError(err)
		s.logError(logger.SourceOVH, "Hardware replacement request failed", "serviceName", srv, "component", comp, "error", errMsg)
		if strings.Contains(errMsg, "Action pending") {
			ticketID := "unknown"
			if m := ticketIDRegex.FindStringSubmatch(errMsg); len(m) == 2 {
				ticketID = m[1]
			}
			writeJSON(w, http.StatusBadRequest, map[string]any{
				"success":   false,
				"error":     fmt.Sprintf("A hardware replacement ticket (#%s) is already pending", ticketID),
				"ticketId":  ticketID,
				"isPending": true,
			})
			return
		}
		writeSuccessError(w, http.StatusInternalServerError, errMsg)
		return
	}

	s.logWarn(logger.SourceOVH, "Submitted hardware replacement request", "serviceName", srv, "component", comp)
	writeJSON(w, http.StatusOK, map[string]any{
		"success": true,
		"message": "Hardware replacement request submitted",
		"task":    result,
	})
}

// HandleGetServiceInfo handles GET /api/server-control/{serviceName}/serviceinfo.
func (s *Service) HandleGetServiceInfo(w http.ResponseWriter, r *http.Request) {
	client, _, ok := s.requireOVHClient(w, r)
	if !ok {
		return
	}
	srv, ok := requireServiceName(w, r)
	if !ok {
		return
	}

	var info map[string]any
	if err := client.GetWithContext(r.Context(), servicePath(srv)+"/serviceInfos", &info); err != nil {
		writeSuccessError(w, http.StatusInternalServerError, formatOVHError(err))
		return
	}

	renewObj := asMap(info["renew"])
	status := asString(info["status"])
	if status == "" {
		status = "unknown"
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"success": true,
		"serviceInfo": map[string]any{
			"status":        status,
			"expiration":    asString(info["expiration"]),
			"creation":      asString(info["creation"]),
			"renewalType":   asBool(renewObj["automatic"]),
			"renewalPeriod": asInt(renewObj["period"], 0),
		},
	})
}

// HandleGetInterventions handles GET /api/server-control/{serviceName}/interventions.
func (s *Service) HandleGetInterventions(w http.ResponseWriter, r *http.Request) {
	client, _, ok := s.requireOVHClient(w, r)
	if !ok {
		return
	}
	srv, ok := requireServiceName(w, r)
	if !ok {
		return
	}

	ctx := r.Context()
	enc := servicePath(srv)
	var ids []any
	if err := client.GetWithContext(ctx, enc+"/intervention", &ids); err != nil {
		writeSuccessError(w, http.StatusInternalServerError, formatOVHError(err))
		return
	}

	interventions := make([]map[string]any, 0, len(ids))
	for _, id := range ids {
		var detail map[string]any
		if err := client.GetWithContext(ctx, fmt.Sprintf("%s/intervention/%s", enc, url.PathEscape(asString(id))), &detail); err == nil {
			interventions = append(interventions, detail)
		}
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"success":       true,
		"interventions": interventions,
	})
}

// HandleGetInterventionDetail handles GET /api/server-control/{serviceName}/interventions/{interventionId}.
func (s *Service) HandleGetInterventionDetail(w http.ResponseWriter, r *http.Request) {
	client, _, ok := s.requireOVHClient(w, r)
	if !ok {
		return
	}
	srv, ok := requireServiceName(w, r)
	if !ok {
		return
	}
	iid := strings.TrimSpace(chi.URLParam(r, "interventionId"))
	var detail map[string]any
	if err := client.GetWithContext(r.Context(), servicePath(srv)+"/intervention/"+url.PathEscape(iid), &detail); err != nil {
		writeSuccessError(w, http.StatusInternalServerError, formatOVHError(err))
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"success":      true,
		"intervention": detail,
	})
}

// HandleGetPlannedInterventions handles GET /api/server-control/{serviceName}/planned-interventions.
func (s *Service) HandleGetPlannedInterventions(w http.ResponseWriter, r *http.Request) {
	client, _, ok := s.requireOVHClient(w, r)
	if !ok {
		return
	}
	srv, ok := requireServiceName(w, r)
	if !ok {
		return
	}

	ctx := r.Context()
	enc := servicePath(srv)
	var ids []int64
	if err := client.GetWithContext(ctx, enc+"/plannedIntervention", &ids); err != nil {
		writeSuccessError(w, http.StatusInternalServerError, formatOVHError(err))
		return
	}

	list := make([]map[string]any, 0, len(ids))
	for _, id := range ids {
		var detail map[string]any
		if err := client.GetWithContext(ctx, fmt.Sprintf("%s/plannedIntervention/%d", enc, id), &detail); err == nil {
			list = append(list, detail)
		}
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"success":              true,
		"plannedInterventions": list,
	})
}

// HandleGetPlannedInterventionDetail handles GET /api/server-control/{serviceName}/planned-interventions/{interventionId}.
func (s *Service) HandleGetPlannedInterventionDetail(w http.ResponseWriter, r *http.Request) {
	client, _, ok := s.requireOVHClient(w, r)
	if !ok {
		return
	}
	srv, ok := requireServiceName(w, r)
	if !ok {
		return
	}
	iid := strings.TrimSpace(chi.URLParam(r, "interventionId"))
	var detail map[string]any
	if err := client.GetWithContext(r.Context(), servicePath(srv)+"/plannedIntervention/"+url.PathEscape(iid), &detail); err != nil {
		writeSuccessError(w, http.StatusInternalServerError, formatOVHError(err))
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"success":             true,
		"plannedIntervention": detail,
	})
}

// HandleGetOngoingTasks handles GET /api/server-control/{serviceName}/ongoing.
func (s *Service) HandleGetOngoingTasks(w http.ResponseWriter, r *http.Request) {
	client, _, ok := s.requireOVHClient(w, r)
	if !ok {
		return
	}
	srv, ok := requireServiceName(w, r)
	if !ok {
		return
	}
	var ongoing any
	if err := client.GetWithContext(r.Context(), servicePath(srv)+"/ongoing", &ongoing); err != nil {
		writeSuccessError(w, http.StatusInternalServerError, formatOVHError(err))
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"success": true,
		"ongoing": ongoing,
	})
}

// HandleGetCompliantWindows handles GET /api/server-control/{serviceName}/license/windows/compliant.
func (s *Service) HandleGetCompliantWindows(w http.ResponseWriter, r *http.Request) {
	s.handleCompliantLicense(w, r, "/license/compliantWindows")
}

// HandleGetCompliantWindowsSQL handles GET /api/server-control/{serviceName}/license/windows-sql/compliant.
func (s *Service) HandleGetCompliantWindowsSQL(w http.ResponseWriter, r *http.Request) {
	s.handleCompliantLicense(w, r, "/license/compliantWindowsSqlServer")
}

func (s *Service) handleCompliantLicense(w http.ResponseWriter, r *http.Request, suffix string) {
	client, _, ok := s.requireOVHClient(w, r)
	if !ok {
		return
	}
	srv, ok := requireServiceName(w, r)
	if !ok {
		return
	}
	var versions any
	if err := client.GetWithContext(r.Context(), servicePath(srv)+suffix, &versions); err != nil {
		writeSuccessError(w, http.StatusInternalServerError, formatOVHError(err))
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"success":  true,
		"versions": versions,
	})
}

// HandleChangeContact handles POST /api/server-control/{serviceName}/change-contact.
func (s *Service) HandleChangeContact(w http.ResponseWriter, r *http.Request) {
	client, _, ok := s.requireOVHClient(w, r)
	if !ok {
		return
	}
	srv, ok := requireServiceName(w, r)
	if !ok {
		return
	}

	var body struct {
		ContactAdmin   string `json:"contactAdmin"`
		ContactTech    string `json:"contactTech"`
		ContactBilling string `json:"contactBilling"`
	}
	if err := decodeJSONBody(r, &body); err != nil {
		writeSuccessError(w, http.StatusBadRequest, "Invalid JSON payload")
		return
	}

	params := make(map[string]string)
	if v := strings.TrimSpace(body.ContactAdmin); v != "" {
		params["contactAdmin"] = v
	}
	if v := strings.TrimSpace(body.ContactTech); v != "" {
		params["contactTech"] = v
	}
	if v := strings.TrimSpace(body.ContactBilling); v != "" {
		params["contactBilling"] = v
	}
	if len(params) == 0 {
		writeSuccessError(w, http.StatusBadRequest, "At least one contact (contactAdmin, contactTech, or contactBilling) is required")
		return
	}

	var result map[string]any
	if err := client.PostWithContext(r.Context(), servicePath(srv)+"/changeContact", params, &result); err != nil {
		s.logError(logger.SourceOVH, "Failed to submit contact change request", "serviceName", srv, "error", formatOVHError(err))
		writeSuccessError(w, http.StatusInternalServerError, formatOVHError(err))
		return
	}

	s.logWarn(logger.SourceOVH, "Submitted contact change request", "serviceName", srv)
	writeJSON(w, http.StatusOK, map[string]any{
		"success": true,
		"message": "Contact change request submitted",
		"taskId":  result["id"],
		"details": result,
	})
}

// HandleTerminateService handles POST /api/server-control/{serviceName}/terminate.
func (s *Service) HandleTerminateService(w http.ResponseWriter, r *http.Request) {
	client, _, ok := s.requireOVHClient(w, r)
	if !ok {
		return
	}
	srv, ok := requireServiceName(w, r)
	if !ok {
		return
	}

	var result any
	if err := client.PostWithContext(r.Context(), servicePath(srv)+"/terminate", nil, &result); err != nil {
		s.logError(logger.SourceOVH, "Failed to submit termination request", "serviceName", srv, "error", formatOVHError(err))
		writeSuccessError(w, http.StatusInternalServerError, formatOVHError(err))
		return
	}

	s.logWarn(logger.SourceOVH, "Submitted server termination request", "serviceName", srv)
	writeJSON(w, http.StatusOK, map[string]any{
		"success": true,
		"message": "Termination request submitted",
		"result":  result,
	})
}

// HandleConfirmTermination handles POST /api/server-control/{serviceName}/confirm-termination.
func (s *Service) HandleConfirmTermination(w http.ResponseWriter, r *http.Request) {
	client, _, ok := s.requireOVHClient(w, r)
	if !ok {
		return
	}
	srv, ok := requireServiceName(w, r)
	if !ok {
		return
	}

	var body struct {
		Token string `json:"token"`
	}
	if err := decodeJSONBody(r, &body); err != nil || strings.TrimSpace(body.Token) == "" {
		writeSuccessError(w, http.StatusBadRequest, "Confirmation token is required")
		return
	}

	if err := client.PostWithContext(r.Context(), servicePath(srv)+"/confirmTermination", map[string]string{
		"token": strings.TrimSpace(body.Token),
	}, nil); err != nil {
		s.logError(logger.SourceOVH, "Failed to confirm server termination", "serviceName", srv, "error", formatOVHError(err))
		writeSuccessError(w, http.StatusInternalServerError, formatOVHError(err))
		return
	}

	s.logWarn(logger.SourceOVH, "Confirmed dedicated server termination", "serviceName", srv)
	writeJSON(w, http.StatusOK, map[string]any{
		"success": true,
		"message": "Termination confirmed",
	})
}

// HandleGetSPLAList handles GET /api/server-control/{serviceName}/spla.
func (s *Service) HandleGetSPLAList(w http.ResponseWriter, r *http.Request) {
	client, _, ok := s.requireOVHClient(w, r)
	if !ok {
		return
	}
	srv, ok := requireServiceName(w, r)
	if !ok {
		return
	}

	ctx := r.Context()
	enc := servicePath(srv)
	var ids []int64
	if err := client.GetWithContext(ctx, enc+"/spla", &ids); err != nil {
		writeSuccessError(w, http.StatusInternalServerError, formatOVHError(err))
		return
	}

	list := make([]map[string]any, 0, len(ids))
	for _, id := range ids {
		var detail map[string]any
		if err := client.GetWithContext(ctx, fmt.Sprintf("%s/spla/%d", enc, id), &detail); err != nil {
			list = append(list, map[string]any{"id": id})
			continue
		}
		detail["id"] = id
		list = append(list, detail)
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"success":  true,
		"splaList": list,
	})
}

// HandleCreateSPLA handles POST /api/server-control/{serviceName}/spla.
func (s *Service) HandleCreateSPLA(w http.ResponseWriter, r *http.Request) {
	client, _, ok := s.requireOVHClient(w, r)
	if !ok {
		return
	}
	srv, ok := requireServiceName(w, r)
	if !ok {
		return
	}

	var body struct {
		Type         string `json:"type"`
		SerialNumber string `json:"serialNumber"`
	}
	if err := decodeJSONBody(r, &body); err != nil || strings.TrimSpace(body.Type) == "" {
		writeSuccessError(w, http.StatusBadRequest, "type is required")
		return
	}

	var result any
	if err := client.PostWithContext(r.Context(), servicePath(srv)+"/spla", map[string]any{
		"type":         strings.TrimSpace(body.Type),
		"serialNumber": strings.TrimSpace(body.SerialNumber),
	}, &result); err != nil {
		writeSuccessError(w, http.StatusInternalServerError, formatOVHError(err))
		return
	}

	s.logInfo(logger.SourceOVH, "Created SPLA license", "serviceName", srv, "type", body.Type)
	writeJSON(w, http.StatusOK, map[string]any{
		"success": true,
		"message": "SPLA license created",
		"result":  result,
	})
}

// HandleGetBIOSSettings handles GET /api/server-control/{serviceName}/bios-settings.
func (s *Service) HandleGetBIOSSettings(w http.ResponseWriter, r *http.Request) {
	client, _, ok := s.requireOVHClient(w, r)
	if !ok {
		return
	}
	srv, ok := requireServiceName(w, r)
	if !ok {
		return
	}

	var bios any
	if err := client.GetWithContext(r.Context(), servicePath(srv)+"/biosSettings", &bios); err != nil {
		if isOVHNotFound(err) {
			writeSuccessError(w, http.StatusNotFound, "BIOS settings are not available on this server")
			return
		}
		writeSuccessError(w, http.StatusBadGateway, formatOVHError(err))
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"success": true,
		"bios":    bios,
	})
}

// HandleGetBIOSSettingsSGX handles GET /api/server-control/{serviceName}/bios-settings/sgx.
func (s *Service) HandleGetBIOSSettingsSGX(w http.ResponseWriter, r *http.Request) {
	client, _, ok := s.requireOVHClient(w, r)
	if !ok {
		return
	}
	srv, ok := requireServiceName(w, r)
	if !ok {
		return
	}

	var sgx any
	if err := client.GetWithContext(r.Context(), servicePath(srv)+"/biosSettings/sgx", &sgx); err != nil {
		if isOVHNotFound(err) {
			writeSuccessError(w, http.StatusNotFound, "SGX BIOS settings are not available on this server")
			return
		}
		writeSuccessError(w, http.StatusBadGateway, formatOVHError(err))
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"success": true,
		"sgx":     sgx,
	})
}
