package relay

import "runtime"

type PlatformInfo struct {
	OS          string
	Arch        string
	LibraryName string
	Supported   bool
}

func GetPlatformInfo() PlatformInfo {
	info := PlatformInfo{
		OS:   runtime.GOOS,
		Arch: runtime.GOARCH,
	}

	switch runtime.GOOS {
	case "windows":
		switch runtime.GOARCH {
		case "amd64":
			info.LibraryName = "relay_leaf-windows-x64.dll"
			info.Supported = true
		case "386":
			info.LibraryName = "relay_leaf-windows-x86.dll"
			info.Supported = true
		}
	case "linux":
		switch runtime.GOARCH {
		case "amd64":
			info.LibraryName = "librelay_leaf-linux-x64.so"
			info.Supported = true
		case "arm64":
			info.LibraryName = "librelay_leaf-linux-arm64.so"
			info.Supported = true
		}
	case "darwin":
		switch runtime.GOARCH {
		case "arm64":
			info.LibraryName = "librelay_leaf-darwin-arm64.dylib"
			info.Supported = true
		case "amd64":
			info.LibraryName = "librelay_leaf-darwin-amd64.dylib"
			info.Supported = true
		}
	}

	return info
}
