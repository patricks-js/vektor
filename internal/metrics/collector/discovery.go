package collector

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"vektor/internal/host"
)

// FilesystemTarget holds a discovered local filesystem target mapped to its resource_id.
type FilesystemTarget struct {
	ResourceID string
	Mountpoint string
	FSType     string
	Source     string
}

// SensorTarget holds a discovered hwmon temperature channel mapped to its resource_id.
type SensorTarget struct {
	ResourceID string
	Chip       string
	Label      string
	InputPath  string
}

// Topology maintains the latest thread-safe mapping between Linux kernel names/paths and Vektor resource IDs.
type Topology struct {
	mu               sync.RWMutex
	disks            map[string]string
	nics             map[string]NICTarget
	includeInHostNet map[string]bool
	filesystems      []FilesystemTarget
	sensors          []SensorTarget
}

// NewTopology constructs an empty Topology.
func NewTopology() *Topology {
	return &Topology{
		disks:            make(map[string]string),
		nics:             make(map[string]NICTarget),
		includeInHostNet: make(map[string]bool),
	}
}

func (t *Topology) DiskLookup() map[string]string {
	t.mu.RLock()
	defer t.mu.RUnlock()
	out := make(map[string]string, len(t.disks))
	for k, v := range t.disks {
		out[k] = v
	}
	return out
}

func (t *Topology) NICLookup() map[string]NICTarget {
	t.mu.RLock()
	defer t.mu.RUnlock()
	out := make(map[string]NICTarget, len(t.nics))
	for k, v := range t.nics {
		out[k] = v
	}
	return out
}

func (t *Topology) IncludeNICInHostAggregate(resourceID string) bool {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return t.includeInHostNet[resourceID]
}

func (t *Topology) Filesystems() []FilesystemTarget {
	t.mu.RLock()
	defer t.mu.RUnlock()
	out := make([]FilesystemTarget, len(t.filesystems))
	copy(out, t.filesystems)
	return out
}

func (t *Topology) Sensors() []SensorTarget {
	t.mu.RLock()
	defer t.mu.RUnlock()
	out := make([]SensorTarget, len(t.sensors))
	copy(out, t.sensors)
	return out
}

// DiscoveredDisk describes a discovered block or device-mapper disk.
type DiscoveredDisk struct {
	KernelName string
	DMName     string
	DMUUID     string
	Model      string
	Serial     string
	WWID       string
	Rotational *bool
	SizeBytes  uint64
	Slaves     []string

	ExternalKey  string
	DisplayName  string
	MetadataJSON string
}

// DiscoveredFilesystem describes a discovered local mountpoint.
type DiscoveredFilesystem struct {
	Mountpoint string
	FSType     string
	Source     string
	MajorMinor string
	UUID       string

	ExternalKey  string
	DisplayName  string
	MetadataJSON string
}

// DiscoveredNIC describes a discovered physical or virtual network interface.
type DiscoveredNIC struct {
	Name                   string
	IfIndex                int
	MAC                    string
	OperState              string
	Type                   int
	MTU                    int
	SpeedMbps              int
	PCIAddr                string
	IsVirtual              bool
	IsLoopback             bool
	IsBridge               bool
	IsBond                 bool
	Master                 string
	MasterIsBond           bool
	IncludeInHostAggregate bool

	ExternalKey  string
	DisplayName  string
	MetadataJSON string
	portName     string
	devPort      string
}

// DiscoveredSensor describes a discovered hwmon temperature sensor channel.
type DiscoveredSensor struct {
	HwmonName    string
	Chip         string
	DeviceID     string
	Channel      string
	Label        string
	InputPath    string
	MaxCelsius   *float64
	CritCelsius  *float64
	ExternalKey  string
	DisplayName  string
	MetadataJSON string
}

// Discovery scans /proc, /sys, and /dev/disk to identify disks, filesystems, NICs, and sensors.
type Discovery struct {
	procRoot       string
	sysRoot        string
	devDiskRoot    string
	excludedFS     map[string]bool
	excludeFuse    bool
	includeSensors bool
	topology       *Topology
}

// NewDiscovery constructs a Discovery instance.
func NewDiscovery(procRoot, sysRoot, devDiskRoot string, excludedFSTypes []string, includeSensors bool) *Discovery {
	if procRoot == "" {
		procRoot = "/proc"
	}
	if sysRoot == "" {
		sysRoot = "/sys"
	}
	if devDiskRoot == "" {
		devDiskRoot = "/dev/disk"
	}
	ex := make(map[string]bool, len(excludedFSTypes))
	for _, fs := range excludedFSTypes {
		norm := strings.ToLower(strings.TrimSpace(fs))
		if norm != "" {
			ex[norm] = true
		}
	}
	return &Discovery{
		procRoot:       procRoot,
		sysRoot:        sysRoot,
		devDiskRoot:    devDiskRoot,
		excludedFS:     ex,
		excludeFuse:    ex["fuse"],
		includeSensors: includeSensors,
		topology:       NewTopology(),
	}
}

// Topology returns the live thread-safe Topology snapshot updated by ReconcileAll.
func (d *Discovery) Topology() *Topology {
	return d.topology
}

// DiscoverDisks scans <sysRoot>/block/* excluding partitions and ephemeral loop/ram/zram/sr/fd devices.
func (d *Discovery) DiscoverDisks() ([]DiscoveredDisk, error) {
	blockDir := filepath.Join(d.sysRoot, "block")
	entries, err := os.ReadDir(blockDir)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", blockDir, err)
	}

	var out []DiscoveredDisk
	for _, entry := range entries {
		name := entry.Name()
		if shouldSkipBlockDevice(name) {
			continue
		}
		devPath := filepath.Join(blockDir, name)
		dmName := readTrimmed(filepath.Join(devPath, "dm/name"))
		dmUUID := readTrimmed(filepath.Join(devPath, "dm/uuid"))
		model := readTrimmed(filepath.Join(devPath, "device/model"))
		serial := readTrimmed(filepath.Join(devPath, "device/serial"))
		wwid := readTrimmed(filepath.Join(devPath, "device/wwid"))

		var rotPtr *bool
		if rotRaw := readTrimmed(filepath.Join(devPath, "queue/rotational")); rotRaw == "0" || rotRaw == "1" {
			v := rotRaw == "1"
			rotPtr = &v
		}

		var sizeBytes uint64
		if sizeRaw := readTrimmed(filepath.Join(devPath, "size")); sizeRaw != "" {
			if sectors, err := strconv.ParseUint(sizeRaw, 10, 64); err == nil {
				sizeBytes = sectors * linuxSectorSizeBytes
			}
		}

		var slaves []string
		if slaveEntries, err := os.ReadDir(filepath.Join(devPath, "slaves")); err == nil {
			for _, se := range slaveEntries {
				slaves = append(slaves, se.Name())
			}
			sort.Strings(slaves)
		}

		extKey := diskExternalKey(name, dmUUID, wwid, model, serial)
		display := name
		if dmName != "" {
			display = dmName
		}

		metaObj := map[string]any{
			"kernel_name": name,
			"size_bytes":  sizeBytes,
		}
		if dmName != "" {
			metaObj["dm_name"] = dmName
		}
		if dmUUID != "" {
			metaObj["dm_uuid"] = dmUUID
		}
		if model != "" {
			metaObj["model"] = model
		}
		if serial != "" {
			metaObj["serial"] = serial
		}
		if wwid != "" {
			metaObj["wwid"] = wwid
		}
		if rotPtr != nil {
			metaObj["rotational"] = *rotPtr
		}
		if len(slaves) > 0 {
			metaObj["slaves"] = slaves
		}
		metaBytes, _ := json.Marshal(metaObj)

		out = append(out, DiscoveredDisk{
			KernelName:   name,
			DMName:       dmName,
			DMUUID:       dmUUID,
			Model:        model,
			Serial:       serial,
			WWID:         wwid,
			Rotational:   rotPtr,
			SizeBytes:    sizeBytes,
			Slaves:       slaves,
			ExternalKey:  extKey,
			DisplayName:  display,
			MetadataJSON: string(metaBytes),
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].KernelName < out[j].KernelName })
	return out, nil
}

func shouldSkipBlockDevice(name string) bool {
	for _, prefix := range []string{"loop", "ram", "zram", "sr", "fd"} {
		if strings.HasPrefix(name, prefix) {
			return true
		}
	}
	return false
}

func diskExternalKey(name, dmUUID, wwid, model, serial string) string {
	switch {
	case dmUUID != "":
		return "dm:" + dmUUID
	case wwid != "":
		return "wwid:" + wwid
	case serial != "":
		if model != "" {
			return "serial:" + model + ":" + serial
		}
		return "serial:" + serial
	default:
		return "block:" + name
	}
}

// DiscoverFilesystems parses <procRoot>/self/mountinfo, filters excluded FS types,
// deduplicates bind mounts by (major:minor, root), and resolves UUIDs when available.
func (d *Discovery) DiscoverFilesystems() ([]DiscoveredFilesystem, error) {
	mountData, err := readFileBounded(filepath.Join(d.procRoot, "self/mountinfo"), maxProcSysFileBytes)
	if err != nil {
		return nil, fmt.Errorf("read mountinfo: %w", err)
	}

	uuidByDev := d.buildDeviceUUIDMap()

	type bindKey struct {
		majMin string
		root   string
	}
	dedup := make(map[bindKey]DiscoveredFilesystem)

	scanner := bufio.NewScanner(bytes.NewReader(mountData))
	for scanner.Scan() {
		line := scanner.Text()
		left, right, ok := strings.Cut(line, " - ")
		if !ok {
			continue
		}
		lf := strings.Fields(left)
		rf := strings.Fields(right)
		if len(lf) < 5 || len(rf) < 2 {
			continue
		}

		majMin := lf[2]
		root := unescapeOctal(lf[3])
		mountpoint := unescapeOctal(lf[4])
		fsType := strings.ToLower(rf[0])
		source := unescapeOctal(rf[1])

		if d.isExcludedFS(fsType) {
			continue
		}

		uuid := d.resolveUUID(source, uuidByDev)
		extKey := "dev:" + source + ":" + mountpoint
		if uuid != "" {
			extKey = "uuid:" + uuid + ":" + mountpoint
		}

		metaObj := map[string]any{
			"mountpoint":  mountpoint,
			"fs_type":     fsType,
			"source":      source,
			"major_minor": majMin,
		}
		if uuid != "" {
			metaObj["uuid"] = uuid
		}
		metaBytes, _ := json.Marshal(metaObj)

		cand := DiscoveredFilesystem{
			Mountpoint:   mountpoint,
			FSType:       fsType,
			Source:       source,
			MajorMinor:   majMin,
			UUID:         uuid,
			ExternalKey:  extKey,
			DisplayName:  mountpoint,
			MetadataJSON: string(metaBytes),
		}

		bk := bindKey{majMin: majMin, root: root}
		if prev, exists := dedup[bk]; !exists || len(cand.Mountpoint) < len(prev.Mountpoint) ||
			(len(cand.Mountpoint) == len(prev.Mountpoint) && cand.Mountpoint < prev.Mountpoint) {
			dedup[bk] = cand
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("scan mountinfo: %w", err)
	}

	out := make([]DiscoveredFilesystem, 0, len(dedup))
	for _, fs := range dedup {
		out = append(out, fs)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Mountpoint < out[j].Mountpoint })
	return out, nil
}

func (d *Discovery) isExcludedFS(fsType string) bool {
	if d.excludedFS[fsType] {
		return true
	}
	if d.excludeFuse && strings.HasPrefix(fsType, "fuse.") {
		return true
	}
	return false
}

func (d *Discovery) buildDeviceUUIDMap() map[string]string {
	m := make(map[string]string)
	byUUIDDir := filepath.Join(d.devDiskRoot, "by-uuid")
	entries, err := os.ReadDir(byUUIDDir)
	if err != nil {
		return m
	}

	// Map dm-<N> -> /dev/mapper/<dmName> so mountinfo /dev/mapper/* sources match by-uuid links to dm-*.
	dmAliasByKernel := make(map[string]string)
	if blockEntries, err := os.ReadDir(filepath.Join(d.sysRoot, "block")); err == nil {
		for _, be := range blockEntries {
			if dmName := readTrimmed(filepath.Join(d.sysRoot, "block", be.Name(), "dm/name")); dmName != "" {
				dmAliasByKernel[be.Name()] = "/dev/mapper/" + dmName
			}
		}
	}

	for _, entry := range entries {
		uuid := entry.Name()
		linkPath := filepath.Join(byUUIDDir, uuid)
		target, err := os.Readlink(linkPath)
		if err != nil {
			continue
		}
		resolved := target
		if !filepath.IsAbs(resolved) {
			resolved = filepath.Clean(filepath.Join(byUUIDDir, target))
		}
		m[resolved] = uuid
		base := filepath.Base(resolved)
		m["/dev/"+base] = uuid
		m["base:"+base] = uuid
		if alias, ok := dmAliasByKernel[base]; ok {
			m[alias] = uuid
		}
	}
	return m
}

func (d *Discovery) resolveUUID(source string, uuidByDev map[string]string) string {
	if len(uuidByDev) == 0 || source == "" {
		return ""
	}
	if u, ok := uuidByDev[source]; ok {
		return u
	}
	if resolved, err := filepath.EvalSymlinks(source); err == nil {
		if u, ok := uuidByDev[resolved]; ok {
			return u
		}
		if u, ok := uuidByDev["base:"+filepath.Base(resolved)]; ok {
			return u
		}
	}
	if strings.HasPrefix(source, "/dev/") {
		if u, ok := uuidByDev["base:"+filepath.Base(source)]; ok {
			return u
		}
	}
	return ""
}

// DiscoverNICs scans <sysRoot>/class/net/* and computes virtual/bond/bridge roles and aggregate eligibility.
func (d *Discovery) DiscoverNICs() ([]DiscoveredNIC, error) {
	netDir := filepath.Join(d.sysRoot, "class/net")
	entries, err := os.ReadDir(netDir)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", netDir, err)
	}

	raw := make([]DiscoveredNIC, 0, len(entries))
	byName := make(map[string]*DiscoveredNIC, len(entries))

	for _, entry := range entries {
		name := entry.Name()
		ifaceDir := filepath.Join(netDir, name)
		info, err := os.Stat(ifaceDir)
		if err != nil || !info.IsDir() {
			continue
		}

		ifIdx, _ := strconv.Atoi(readTrimmed(filepath.Join(ifaceDir, "ifindex")))
		mac := strings.ToLower(readTrimmed(filepath.Join(ifaceDir, "address")))
		oper := readTrimmed(filepath.Join(ifaceDir, "operstate"))
		nicType, _ := strconv.Atoi(readTrimmed(filepath.Join(ifaceDir, "type")))
		mtu, _ := strconv.Atoi(readTrimmed(filepath.Join(ifaceDir, "mtu")))
		speed := -1
		if sp, err := strconv.Atoi(readTrimmed(filepath.Join(ifaceDir, "speed"))); err == nil && sp > 0 {
			speed = sp
		}

		isVirtual := false
		pciAddr := ""
		if devTarget, err := os.Readlink(filepath.Join(ifaceDir, "device")); err != nil {
			isVirtual = true
		} else if strings.Contains(devTarget, "/virtual/") {
			isVirtual = true
		} else {
			pciAddr = filepath.Base(devTarget)
		}

		isLoopback := nicType == 772 || name == "lo"
		isBridge := dirExists(filepath.Join(ifaceDir, "bridge"))
		isBond := dirExists(filepath.Join(ifaceDir, "bonding"))
		master := ""
		if mTarget, err := os.Readlink(filepath.Join(ifaceDir, "master")); err == nil {
			master = filepath.Base(mTarget)
		}

		nic := DiscoveredNIC{
			Name:       name,
			IfIndex:    ifIdx,
			MAC:        mac,
			OperState:  oper,
			Type:       nicType,
			MTU:        mtu,
			SpeedMbps:  speed,
			PCIAddr:    pciAddr,
			IsVirtual:  isVirtual,
			IsLoopback: isLoopback,
			IsBridge:   isBridge,
			IsBond:     isBond,
			Master:     master,
			portName:   readTrimmed(filepath.Join(ifaceDir, "phys_port_name")),
			devPort:    readTrimmed(filepath.Join(ifaceDir, "dev_port")),
		}
		raw = append(raw, nic)
	}

	for i := range raw {
		byName[raw[i].Name] = &raw[i]
	}

	for i := range raw {
		nic := &raw[i]
		if nic.Master != "" {
			if m, ok := byName[nic.Master]; ok && m.IsBond {
				nic.MasterIsBond = true
			}
		}

		switch {
		case nic.IsLoopback || nic.IsBridge:
			nic.IncludeInHostAggregate = false
		case nic.IsBond:
			nic.IncludeInHostAggregate = !nic.MasterIsBond
		case nic.IsVirtual:
			nic.IncludeInHostAggregate = false
		default:
			nic.IncludeInHostAggregate = !nic.MasterIsBond
		}

		nic.ExternalKey = nicExternalKey(*nic)
		nic.DisplayName = nic.Name

		metaObj := map[string]any{
			"iface":                     nic.Name,
			"ifindex":                   nic.IfIndex,
			"mac":                       nic.MAC,
			"operstate":                 nic.OperState,
			"type":                      nic.Type,
			"mtu":                       nic.MTU,
			"is_virtual":                nic.IsVirtual,
			"is_loopback":               nic.IsLoopback,
			"is_bridge":                 nic.IsBridge,
			"is_bond":                   nic.IsBond,
			"include_in_host_aggregate": nic.IncludeInHostAggregate,
		}
		if nic.SpeedMbps > 0 {
			metaObj["speed_mbps"] = nic.SpeedMbps
		}
		if nic.PCIAddr != "" {
			metaObj["pci_addr"] = nic.PCIAddr
			metaObj["phys_port_name"] = nic.portName
			metaObj["dev_port"] = nic.devPort
		}
		if nic.Master != "" {
			metaObj["master"] = nic.Master
		}
		metaBytes, _ := json.Marshal(metaObj)
		nic.MetadataJSON = string(metaBytes)
	}

	disambiguateNICs(raw, nil)
	sort.Slice(raw, func(i, j int) bool { return raw[i].Name < raw[j].Name })
	return raw, nil
}

func nicExternalKey(nic DiscoveredNIC) string {
	if !nic.IsVirtual && nic.PCIAddr != "" {
		return "pci:" + nic.PCIAddr
	}
	if !nic.IsVirtual && isUsableMAC(nic.MAC) {
		return "mac:" + nic.MAC
	}
	return "virtual:" + nic.Name
}

func isUsableMAC(mac string) bool {
	return mac != "" && mac != "00:00:00:00:00:00"
}

func nicPortKeys(nic DiscoveredNIC, base string) []string {
	var keys []string
	if nic.portName != "" {
		keys = append(keys, base+"/port:"+nic.portName)
	}
	if nic.devPort != "" {
		keys = append(keys, base+"/dev_port:"+nic.devPort)
	}
	if isUsableMAC(nic.MAC) {
		keys = append(keys, base+"/mac:"+nic.MAC)
	}
	return append(keys, base+"/iface:"+nic.Name)
}

func disambiguateNICs(nics []DiscoveredNIC, existing []host.Resource) {
	byKey := make(map[string]host.Resource, len(existing))
	split := make(map[string]bool)
	for _, resource := range existing {
		byKey[resource.ExternalKey] = resource
		if base, _, ok := strings.Cut(resource.ExternalKey, "/"); ok {
			split[base] = true
		}
	}
	counts := make(map[string]int)
	legacyOwners := make(map[string][]int)
	for i, nic := range nics {
		base := nicExternalKey(nic)
		counts[base]++
		for _, key := range nicPortKeys(nic, base) {
			counts[key]++
		}
		var old struct {
			MAC      string `json:"mac"`
			PortName string `json:"phys_port_name"`
			DevPort  string `json:"dev_port"`
		}
		if resource, ok := byKey[base]; ok && json.Unmarshal([]byte(resource.MetadataJSON), &old) == nil {
			if (old.PortName != "" && old.PortName == nic.portName) ||
				(old.DevPort != "" && old.DevPort == nic.devPort) ||
				(isUsableMAC(old.MAC) && old.MAC == nic.MAC) ||
				(!isUsableMAC(old.MAC) && old.PortName == "" && old.DevPort == "" && resource.DisplayName == nic.Name) {
				legacyOwners[base] = append(legacyOwners[base], i)
			}
		}
	}
	for i, nic := range nics {
		base := nicExternalKey(nic)
		if !split[base] && counts[base] == 1 {
			nics[i].ExternalKey = base
			continue
		}
		var key, prior string
		for _, candidate := range nicPortKeys(nic, base) {
			if counts[candidate] != 1 {
				continue
			}
			if key == "" {
				key = candidate
			}
			if _, exists := byKey[candidate]; exists {
				prior = candidate
				break
			}
		}
		owners := legacyOwners[base]
		switch {
		case prior != "":
			key = prior
		case len(owners) == 1 && owners[0] == i:
			key = base
		}
		nics[i].ExternalKey = key
	}
}

// DiscoverSensors scans <sysRoot>/class/hwmon/hwmon* for temperature inputs (`temp*_input`).
func (d *Discovery) DiscoverSensors() ([]DiscoveredSensor, error) {
	if !d.includeSensors {
		return nil, nil
	}
	hwmonRoot := filepath.Join(d.sysRoot, "class/hwmon")
	entries, err := os.ReadDir(hwmonRoot)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, fmt.Errorf("read %s: %w", hwmonRoot, err)
	}

	var out []DiscoveredSensor
	for _, entry := range entries {
		hwDir := filepath.Join(hwmonRoot, entry.Name())
		chip := readTrimmed(filepath.Join(hwDir, "name"))
		if chip == "" {
			chip = entry.Name()
		}
		devID := "nodev"
		if devTarget, err := os.Readlink(filepath.Join(hwDir, "device")); err == nil {
			if base := filepath.Base(devTarget); base != "" && base != "." {
				devID = base
			}
		}

		files, err := os.ReadDir(hwDir)
		if err != nil {
			continue
		}
		for _, f := range files {
			fname := f.Name()
			if !strings.HasPrefix(fname, "temp") || !strings.HasSuffix(fname, "_input") {
				continue
			}
			channel := strings.TrimSuffix(fname, "_input")
			label := readTrimmed(filepath.Join(hwDir, channel+"_label"))
			display := chip + " " + channel
			if label != "" {
				display = chip + ": " + label
			}

			var maxPtr, critPtr *float64
			if v, ok := readMilliDegrees(filepath.Join(hwDir, channel+"_max")); ok {
				maxPtr = &v
			}
			if v, ok := readMilliDegrees(filepath.Join(hwDir, channel+"_crit")); ok {
				critPtr = &v
			}

			extKey := fmt.Sprintf("hwmon:%s:%s:%s", chip, devID, channel)
			metaObj := map[string]any{
				"hwmon":   entry.Name(),
				"chip":    chip,
				"dev_id":  devID,
				"channel": channel,
			}
			if label != "" {
				metaObj["label"] = label
			}
			if maxPtr != nil {
				metaObj["max_celsius"] = *maxPtr
			}
			if critPtr != nil {
				metaObj["crit_celsius"] = *critPtr
			}
			metaBytes, _ := json.Marshal(metaObj)

			out = append(out, DiscoveredSensor{
				HwmonName:    entry.Name(),
				Chip:         chip,
				DeviceID:     devID,
				Channel:      channel,
				Label:        label,
				InputPath:    filepath.Join(hwDir, fname),
				MaxCelsius:   maxPtr,
				CritCelsius:  critPtr,
				ExternalKey:  extKey,
				DisplayName:  display,
				MetadataJSON: string(metaBytes),
			})
		}
	}

	sort.Slice(out, func(i, j int) bool { return out[i].ExternalKey < out[j].ExternalKey })
	return out, nil
}

// ReconcileAll refreshes each resource kind after its registry transaction succeeds.
func (d *Discovery) ReconcileAll(ctx context.Context, reg *host.Registry, nowMs int64, includePerCoreCPU bool) error {
	var errs []error

	if includePerCoreCPU {
		samples, err := readCPUStat(d.procRoot, true, time.Time{}, 0)
		if err != nil {
			errs = append(errs, fmt.Errorf("discover cpu_cores: %w", err))
		} else {
			var cores []string
			for _, sample := range samples {
				if sample.ResourceID != host.HostResourceID {
					cores = append(cores, sample.Core)
				}
			}
			if _, err := reg.EnsureCPUCores(ctx, cores, nowMs); err != nil {
				errs = append(errs, fmt.Errorf("reconcile cpu_cores: %w", err))
			}
		}
	}

	diskMap := make(map[string]string)
	if disks, err := d.DiscoverDisks(); err != nil {
		errs = append(errs, fmt.Errorf("discover disks: %w", err))
	} else {
		items := make([]host.DiscoveredResource, 0, len(disks))
		for _, dk := range disks {
			items = append(items, host.DiscoveredResource{
				ExternalKey:  dk.ExternalKey,
				DisplayName:  dk.DisplayName,
				MetadataJSON: dk.MetadataJSON,
			})
		}
		if reconciled, err := reg.Reconcile(ctx, host.KindDisk, items, nowMs); err != nil {
			errs = append(errs, fmt.Errorf("reconcile disks: %w", err))
		} else {
			byExt := indexIDByExt(reconciled)
			for _, dk := range disks {
				if id, ok := byExt[dk.ExternalKey]; ok {
					diskMap[dk.KernelName] = id
				}
			}
			d.topology.mu.Lock()
			d.topology.disks = diskMap
			d.topology.mu.Unlock()
		}
	}

	if err := ctx.Err(); err != nil {
		return err
	}
	var fsTargets []FilesystemTarget
	if filesystems, err := d.DiscoverFilesystems(); err != nil {
		errs = append(errs, fmt.Errorf("discover filesystems: %w", err))
	} else {
		items := make([]host.DiscoveredResource, 0, len(filesystems))
		for _, fs := range filesystems {
			items = append(items, host.DiscoveredResource{
				ExternalKey:  fs.ExternalKey,
				DisplayName:  fs.DisplayName,
				MetadataJSON: fs.MetadataJSON,
			})
		}
		if reconciled, err := reg.Reconcile(ctx, host.KindFilesystem, items, nowMs); err != nil {
			errs = append(errs, fmt.Errorf("reconcile filesystems: %w", err))
		} else {
			byExt := indexIDByExt(reconciled)
			for _, fs := range filesystems {
				if id, ok := byExt[fs.ExternalKey]; ok {
					fsTargets = append(fsTargets, FilesystemTarget{
						ResourceID: id,
						Mountpoint: fs.Mountpoint,
						FSType:     fs.FSType,
						Source:     fs.Source,
					})
				}
			}
			d.topology.mu.Lock()
			d.topology.filesystems = fsTargets
			d.topology.mu.Unlock()
		}
	}

	if err := ctx.Err(); err != nil {
		return err
	}
	nicMap := make(map[string]NICTarget)
	includeNetMap := make(map[string]bool)
	if nics, err := d.DiscoverNICs(); err != nil {
		errs = append(errs, fmt.Errorf("discover nics: %w", err))
	} else if existing, err := reg.List(ctx, host.KindNIC, true); err != nil {
		errs = append(errs, fmt.Errorf("read existing nics: %w", err))
	} else {
		disambiguateNICs(nics, existing)
		items := make([]host.DiscoveredResource, 0, len(nics))
		for _, nic := range nics {
			items = append(items, host.DiscoveredResource{
				ExternalKey:  nic.ExternalKey,
				DisplayName:  nic.DisplayName,
				MetadataJSON: nic.MetadataJSON,
			})
		}
		if reconciled, err := reg.Reconcile(ctx, host.KindNIC, items, nowMs); err != nil {
			errs = append(errs, fmt.Errorf("reconcile nics: %w", err))
		} else {
			byExt := indexIDByExt(reconciled)
			for _, nic := range nics {
				if id, ok := byExt[nic.ExternalKey]; ok {
					nicMap[nic.Name] = NICTarget{
						ResourceID:             id,
						IncludeInHostAggregate: nic.IncludeInHostAggregate,
					}
					includeNetMap[id] = nic.IncludeInHostAggregate
				}
			}
			d.topology.mu.Lock()
			d.topology.nics, d.topology.includeInHostNet = nicMap, includeNetMap
			d.topology.mu.Unlock()
		}
	}

	if err := ctx.Err(); err != nil {
		return err
	}
	var sensorTargets []SensorTarget
	if sensors, err := d.DiscoverSensors(); err != nil {
		errs = append(errs, fmt.Errorf("discover sensors: %w", err))
	} else {
		items := make([]host.DiscoveredResource, 0, len(sensors))
		for _, s := range sensors {
			items = append(items, host.DiscoveredResource{
				ExternalKey:  s.ExternalKey,
				DisplayName:  s.DisplayName,
				MetadataJSON: s.MetadataJSON,
			})
		}
		if reconciled, err := reg.Reconcile(ctx, host.KindSensor, items, nowMs); err != nil {
			errs = append(errs, fmt.Errorf("reconcile sensors: %w", err))
		} else {
			byExt := indexIDByExt(reconciled)
			for _, s := range sensors {
				if id, ok := byExt[s.ExternalKey]; ok {
					sensorTargets = append(sensorTargets, SensorTarget{
						ResourceID: id,
						Chip:       s.Chip,
						Label:      s.Label,
						InputPath:  s.InputPath,
					})
				}
			}
			d.topology.mu.Lock()
			d.topology.sensors = sensorTargets
			d.topology.mu.Unlock()
		}
	}

	return errors.Join(errs...)
}

func indexIDByExt(list []host.Resource) map[string]string {
	m := make(map[string]string, len(list))
	for _, r := range list {
		m[r.ExternalKey] = r.ID
	}
	return m
}

func readTrimmed(path string) string {
	b, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(b))
}

func dirExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}

func readMilliDegrees(path string) (float64, bool) {
	raw := readTrimmed(path)
	if raw == "" {
		return 0, false
	}
	v, err := strconv.ParseInt(raw, 10, 64)
	if err != nil {
		return 0, false
	}
	return float64(v) / 1000.0, true
}

func unescapeOctal(s string) string {
	if !strings.ContainsRune(s, '\\') {
		return s
	}
	var b strings.Builder
	b.Grow(len(s))
	for i := 0; i < len(s); i++ {
		if s[i] == '\\' && i+3 < len(s) &&
			s[i+1] >= '0' && s[i+1] <= '7' &&
			s[i+2] >= '0' && s[i+2] <= '7' &&
			s[i+3] >= '0' && s[i+3] <= '7' {
			val := (s[i+1]-'0')*64 + (s[i+2]-'0')*8 + (s[i+3] - '0')
			b.WriteByte(val)
			i += 3
			continue
		}
		b.WriteByte(s[i])
	}
	return b.String()
}
