package roce

import (
	"context"
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"strings"

	log "github.com/baidubce/baiducloud-cce-cni-driver/pkg/util/logger"
	"k8s.io/klog"
)

var ( //for mock
	OSReadFile        = os.ReadFile
	OSReadDir         = os.ReadDir
	LinkLayerRoCE     = "Ethernet"
	LinkLayerIB       = "InfiniBand"
	LinkLayerFileName = "link_layer"
	InfiniBandBase    = "/sys/class/infiniband"
)

type IRoCEProbe interface {
	HasRoCEMellanox8Available(ctx context.Context) bool
}

type Probe struct {
	CmdlineShowGids string
	IRoCEProbe
}

func ProbeNew() *Probe {
	var res *Probe

	res = &Probe{
		CmdlineShowGids: "/usr/sbin/show_gids",
	}

	return res
}

type DevInfo struct {
	DevName   string
	NICName   string
	IPAddrStr string
	GIDStr    string
	VerStr    string
	Port      int
	Index     int
}

// const (
// 	GET_ROCE_DEV_INF_MODE_WITH_MELLANOX       = 0x10
// 	GET_ROCE_DEV_INF_MODE_WITH_TYPE_ROCE_ONLY = 0x20
// 	GETrOCE_DEV_INF_MODE_WITH_IP_ADDR        = 0x40
// 	GET_ROCE_DEV_INF_MODE_WITH_ROCE_NUM       = 0x40
// )

/** !!!NOTICE!!! vendor id MUST be in lowercase!!! */
var ROCEDevMellanoxPciVendorIDs = map[string]bool{
	"15b3": true,
}

/** get roce module info from proc/modules */
const kmodProcfsPath = "/proc/modules"

type ModuleAttributeMap map[string](map[string]bool)

func getLoadedModules() (ModuleAttributeMap, error) {
	var res ModuleAttributeMap

	out, err := OSReadFile(kmodProcfsPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read file %s: %s", kmodProcfsPath, err.Error())
	}

	lines := strings.Split(string(out), "\n")
	res = make(ModuleAttributeMap)
	for _, line := range lines {
		// skip empty lines
		if len(line) == 0 {
			continue
		}
		// append loaded module
		fields := strings.Fields(line)
		if len(fields) < 4 {
			continue
		}

		modname := fields[0]
		modparamsline := strings.TrimSpace(fields[3])
		modparams := strings.Split(modparamsline, ",")

		mModparamsmap := make(map[string]bool)
		for _, param := range modparams {
			if len(param) <= 0 {
				continue
			}

			mModparamsmap[param] = true
		}

		res[modname] = mModparamsmap
	}
	return res, nil
}

/** get roce device info from sysfs/pci */
var mandatoryDevAttrs = []string{"class", "vendor", "device", "subsystem_vendor", "subsystem_device"}
var optionalDevAttrs = []string{}

//var optionalDevAttrs = []string{"sriov_totalvfs", "iommu_group/type", "iommu/intel-iommu/version"}

const pcidevicePath = "/sys/bus/pci/devices"

type DeviceAttribute map[string]string /** Example: ["vendor"]:"8086" */

// Read a single PCI device attribute
// A PCI attribute in this context, maps to the corresponding sysfs file
func readSinglePciAttribute(devPath string, attrName string) (string, error) {
	data, err := OSReadFile(filepath.Join(devPath, attrName))
	if err != nil {
		return "", fmt.Errorf("failed to read device attribute %s: %v", attrName, err)
	}
	// Strip whitespace and '0x' prefix
	attrVal := strings.TrimSpace(strings.TrimPrefix(string(data), "0x"))

	if attrName == "class" && len(attrVal) > 4 {
		// Take four first characters, so that the programming
		// interface identifier gets stripped from the raw class code
		attrVal = attrVal[0:4]
	}
	return attrVal, nil
}

// Read information of one PCI device
func readPciDevInfo(devPath string) (*DeviceAttribute, error) {
	attrs := DeviceAttribute{}
	for _, attr := range mandatoryDevAttrs {
		attrVal, err := readSinglePciAttribute(devPath, attr)
		if err != nil {
			return nil, fmt.Errorf("failed to read device[%s] in path[%s]. err:[%s]", attr, devPath, err)
		}
		attrs[attr] = attrVal
	}
	for _, attr := range optionalDevAttrs {
		attrVal, err := readSinglePciAttribute(devPath, attr)
		if err == nil {
			attrs[attr] = attrVal
		}
	}
	return &attrs, nil
}

// detectPci detects available PCI devices and retrieves their device attributes.
// An error is returned if reading any of the mandatory attributes fails.
func detectPci(ctx context.Context) ([]DeviceAttribute, error) {
	sysfsBasePath := pcidevicePath

	devices, err := OSReadDir(sysfsBasePath)
	if err != nil {
		log.V(10).Infof(ctx, "failed on readdir:[%s] err:[%+v]", sysfsBasePath, err)
		return nil, err
	}

	// Iterate over devices
	devInfo := make([]DeviceAttribute, 0, len(devices))
	for _, device := range devices {

		info, err := readPciDevInfo(filepath.Join(sysfsBasePath, device.Name()))
		if err != nil {
			klog.Error(err)
			continue
		}
		devInfo = append(devInfo, *info)
	}

	return devInfo, nil
}

const MellanoxDevOnBoardCountMin int = (1)

func (probe *Probe) RoCEHasMellanoxDevOnBoard(ctx context.Context) (bool, error) {
	var attrs []DeviceAttribute
	var attr DeviceAttribute
	//	var err error
	var b bool
	var key string
	var count int = 0
	var err error

	attrs, err = detectPci(ctx)
	if err != nil {
		return false, err
	}

	key = "vendor"
	for _, attr = range attrs {
		sDevVendorID := strings.ToLower(attr[key])
		if _, b = ROCEDevMellanoxPciVendorIDs[sDevVendorID]; !b {
			continue
		}

		count++
	}

	if count >= MellanoxDevOnBoardCountMin {
		return true, nil
	}

	return false, nil
}

func (probe *Probe) RoCEHasMellanoxModuleLoaded(ctx context.Context) (bool, error) {
	var mamap ModuleAttributeMap
	var amap map[string]bool
	var err error
	//	var err error
	var b bool

	var modulename = "mlx_compat" //TODO
	var attrs = []string{"ib_uverbs", "rdma_ucm"}

	mamap, err = getLoadedModules()
	if err != nil {
		return false, err
	}

	if amap, b = mamap[modulename]; !b {
		return false, nil
	}

	for _, attr := range attrs {
		if _, b = amap[attr]; !b {
			return false, nil
		}
	}

	return true, nil
}

func (probe *Probe) HasRoCEMellanox8Available(ctx context.Context) bool {
	var bret bool
	var err error

	// disable temporily
	//	rret, err = probe.RoCEGetDevInfoByIBCmdlineTools(0)
	//	if err != nil {
	//		log.Errorf(ctx, "failed to get roce dev info: %v", err)
	//		return false
	//	}

	bret, err = probe.RoCEHasMellanoxDevOnBoard(ctx)
	if err != nil {
		log.Errorf(ctx, "failed get roce dev info. err:[%+v]", err)
		return false
	}
	if !bret {
		log.Infof(ctx, "no RoCE device on board")
		return false
	}
	bret, err = probe.RoCEHasMellanoxModuleLoaded(ctx)
	if err != nil {
		log.Errorf(ctx, "failed loading module info. err:[%+v]", err)
		return false
	}
	if !bret {
		log.Infof(ctx, "no RoCE module loaded.")
		return false
	}

	isRoce, err := probe.mellanoxDevIsRoce(ctx, LinkLayerFileName, InfiniBandBase)
	if !isRoce {
		if err != nil {
			log.Errorf(ctx, "mellanoxDevIsRoce error: %s", err.Error())
		} else {
			log.Infof(ctx, "Mellanox8 Device Type Is IB.")
		}
		return false
	}
	log.Infof(ctx, "RoCE Mellanox8 Available.")
	return true
}

func (probe *Probe) mellanoxDevIsRoce(ctx context.Context, filename, dir string) (bool, error) {
	files, err := ioutil.ReadDir(dir)
	if err != nil {
		return false, err
	}
	var RoceIBMap = map[string]bool{
		LinkLayerRoCE: false,
		LinkLayerIB:   false,
	}
	for _, curfile := range files {
		err = probe.mellanoxDevIsRoceCore(filename, dir+"/"+curfile.Name(), RoceIBMap)
		if err != nil {
			log.Errorf(ctx, "mellanoxDevIsRoceCore error: %s", err.Error())
		}
	}
	if ib, _ := RoceIBMap[LinkLayerIB]; ib {
		return false, nil
	}
	if roce, _ := RoceIBMap[LinkLayerRoCE]; roce {
		return true, nil
	}
	return false, nil
}

func (probe *Probe) mellanoxDevIsRoceCore(filename, dir string, m map[string]bool) error {
	files, err := ioutil.ReadDir(dir)
	if err != nil {
		return err
	}

	for _, curfile := range files {
		if curfile.IsDir() {
			_ = probe.mellanoxDevIsRoceCore(filename, dir+"/"+curfile.Name(), m)
		} else {
			if curfile.Name() == filename {
				fileContent, err := ioutil.ReadFile(dir + "/" + filename)
				if err != nil {
					return fmt.Errorf("error reading link_layer file %q: %v", dir+"/"+filename, err)
				}
				if strings.TrimSpace(string(fileContent)) == LinkLayerRoCE {
					m[LinkLayerRoCE] = true
				}
				if strings.TrimSpace(string(fileContent)) == LinkLayerIB {
					m[LinkLayerIB] = true
				}
				return nil
			}
		}
	}
	return nil
}

/** get roce device info from by ib cmdlinetools
20221024 DISABLED TEMPORARILY
*/
//func (probe *Probe) RoCEGetDevInfoByIBCmdlineTools(mode int) ([]DevInfo, error) {
//	var outBuf bytes.Buffer
//	var retCode int
//	var showGidsOutputFieldsNum int = 7
//	var fields []string
//	var resRoCEDevInfoList []DevInfo
//	//	var path string
//	var b bool
//	var f *os.File = nil
//	//	var buf []byte = make([]byte, 128);
//	var pRoCEDevInfo *DevInfo
//	var idx int
//	var i int
//	var err error
//
//	//	/* DEBUG */ cmdline := "/var/log/cce/show_gids"
//
//	cmd := exec.Command(probe.CmdlineShowGids) //TODO context.WithTimeout
//	cmd.Stdout = &outBuf
//	//cmd.Env;
//
//	retCode = 0
//	if err = cmd.Run(); err != nil {
//		if _, b = err.(*exec.ExitError); b {
//			retCode = err.(*exec.ExitError).ExitCode()
//		}
//
//		return nil, err
//	}
//
//	resRoCEDevInfoList = make([]DevInfo, 0, 64)
//	linecnt := 0
//	scanner := bufio.NewScanner(&outBuf)
//	for scanner.Scan() {
//		line := scanner.Text()
//		if linecnt < 2 {
//			goto loop
//		}
//		fields = strings.Split(line, "\t")
//		///** DEBUG */ fmt.Printf("fields:[%v]\n", fields)
//		///** DEBUG */ fmt.Printf("len(fields):[%v]\n", len(fields))
//		if len(fields) != showGidsOutputFieldsNum {
//			goto loop
//		}
//
//		//	OUTPUT of cmd show_gids :
//
//		//	DEV	PORT	INDEX	GID					IPv4  		VER	DEV
//		//	---	----	-----	---					------------  	---	---
//		//	mlx5_0	1	0	fe80:0000:0000:0000:bace:f6ff:fe2e:45e0		0.0.0.0	v1	ens11
//		//	mlx5_0	1	1	fe80:0000:0000:0000:bace:f6ff:fe2e:45e0		8.8.8.8	v2	ens11
//
//		//  [0]		[1]	[2]	[3]										[4]		[5] 	[6]
//
//		pRoCEDevInfo = new(DevInfo)
//		pRoCEDevInfo.DevName = fields[0]
//		pRoCEDevInfo.NICName = fields[6]
//		pRoCEDevInfo.IPAddrStr = fields[4]
//		//TODO check ip in cidr format
//		pRoCEDevInfo.GIDStr = fields[3]
//		pRoCEDevInfo.VerStr = fields[5]
//		idx = 1
//		i, err = strconv.Atoi(fields[idx])
//		if err != nil {
//			log.Warningf(context.TODO(), "BAD RoCE Info: [%s] field:[%d]", line, idx)
//			goto loop
//		}
//		pRoCEDevInfo.Port = i
//
//		idx = 2
//		i, err = strconv.Atoi(fields[idx])
//		if err != nil {
//			log.Warningf(context.TODO(), "BAD RoCE Info: [%s] field:[%d]", line, idx)
//			goto loop
//		}
//		pRoCEDevInfo.Index = i
//		//GET_ROCE_DEV_INF_MODE_WITH_IP_ADDR
//
//		if mode != 0 {
//			if (mode & GET_ROCE_DEV_INF_MODE_WITH_IP_ADDR) != 0 {
//				if len(pRoCEDevInfo.IPAddrStr) <= 0 {
//					goto loop
//				}
//			}
//
//			//			if (mode & GET_ROCE_DEV_INF_MODE_WITH_MELLANOX) != 0 {
//			//				path = fmt.Sprintf("/sys/class/infiniband/%s/device/vendor", pRoCEDevInfo.DevName)
//			//				f, err = os.OpenFile(path, os.O_RDONLY)
//			//				if err != nil {
//			//					goto loop
//			//				}
//			//
//			//				i, err = f.Read(buf)
//			//				if err != nil || i <= 0 {
//			//					if !strings.EqualFold(string(buf), ROCE_DEV_MELLANOX_PCIID) {
//			//						goto loop
//			//					}
//			//				}
//			//			}
//
//			//			if (mode & GET_ROCE_DEV_INF_MODE_WITH_ROCE_NUM) != 0 {
//			//}
//		}
//
//		resRoCEDevInfoList = append(resRoCEDevInfoList, *pRoCEDevInfo)
//	loop:
//		linecnt++
//		if f != nil {
//			f.Close()
//		}
//	}
//
//	_ = retCode
//	return resRoCEDevInfoList, nil
//}
