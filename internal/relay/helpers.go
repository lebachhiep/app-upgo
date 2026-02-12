package relay

import "relay-app/pkg/relayleaf"

func GetLibraryVersion() string {
	return relayleaf.Version()
}
