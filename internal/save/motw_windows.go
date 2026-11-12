package save

import "os"

// zoneIdentifier is what a browser writes for a download: zone 3 is the
// internet. Windows SmartScreen and Office's Protected View read it.
const zoneIdentifier = "[ZoneTransfer]\r\nZoneId=3\r\n"

// markOfTheWeb writes the Zone.Identifier stream of path. path is a file
// this package just created inside the attachment folder, under a cleaned
// name without ':', so the stream name can't point anywhere else.
func markOfTheWeb(path string) error {
	return os.WriteFile(path+":Zone.Identifier", []byte(zoneIdentifier), 0o600)
}
