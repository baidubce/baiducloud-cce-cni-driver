package os

// baidulinux
// cat /etc/os-release
// NAME="BaiduLinux"
// VERSION="3"
// ID="baidulinux"
// ID_LIKE="rhel fedora"
// VERSION_ID="3"
// PLATFORM_ID="platform:bl8"
// PRETTY_NAME="BaiduLinux 3"
// ANSI_COLOR="0;31"
// CPE_NAME="cpe:/o:baidulinux:baidulinux:3"
// HOME_URL="https://centos.org/"
// BUG_REPORT_URL="https://bugs.centos.org/"
// BAIDULINUX_MANTISBT_PROJECT="BaiduLinux
type baidulinux struct {
	*centos
}
