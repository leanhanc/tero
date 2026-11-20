package portcheck

import (
	"strings"

	"github.com/leanhanc/tero/internal/hostfs"
)

// Provider is the cloud a server runs on, as far as its firewall help goes.
type Provider int

const (
	UnknownProvider Provider = iota
	AWS
	GoogleCloud
	Azure
	OracleCloud
)

// azureAssetTag is the chassis asset tag every Azure VM reports.
const azureAssetTag = "7783-7084-3265-9085-8269-3286-77"

// DetectProvider reads the firmware's DMI strings, which each cloud sets to a
// fixed value.
func DetectProvider(files hostfs.FS) Provider {
	vendor := readDMI(files, "sys_vendor")
	assetTag := readDMI(files, "chassis_asset_tag")
	// Older Xen-based EC2 types report vendor Xen, with Amazon in the BIOS
	// version, such as 4.11.amazon.
	isAmazonXen := strings.Contains(strings.ToLower(readDMI(files, "bios_version")), "amazon")

	switch {
	case vendor == "Amazon EC2" || isAmazonXen:
		return AWS
	case vendor == "Google":
		return GoogleCloud
	case vendor == "Microsoft Corporation" && assetTag == azureAssetTag:
		return Azure
	case assetTag == "OracleCloud.com":
		return OracleCloud
	default:
		return UnknownProvider
	}
}

func readDMI(files hostfs.FS, name string) string {
	contents, err := files.ReadFile("/sys/class/dmi/id/" + name)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(contents))
}
