package events

type Type string

const (
	TypeGrabbed           Type = "grabbed"
	TypeDownloadCompleted Type = "download_completed"
	TypeDownloadFailed    Type = "download_failed"
	TypeImported          Type = "imported"
	TypeImportFailed      Type = "import_failed"
	TypeDriftDetected     Type = "drift_detected"
	TypeDriftConfirmed    Type = "drift_confirmed"
	TypeSearched          Type = "searched"
)

func (t Type) Valid() bool {
	switch t {
	case TypeGrabbed, TypeDownloadCompleted, TypeDownloadFailed,
		TypeImported, TypeImportFailed,
		TypeDriftDetected, TypeDriftConfirmed,
		TypeSearched:
		return true
	}
	return false
}
