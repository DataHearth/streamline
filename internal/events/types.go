package events

type Type string

const (
	TypeGrabbed           Type = "grabbed"
	TypeDownloadCompleted Type = "download_completed"
	TypeDownloadFailed    Type = "download_failed"
	TypeDownloadCancelled Type = "download_cancelled"
	TypeGrabWidened       Type = "grab_widened"
	TypeImported          Type = "imported"
	TypeImportFailed      Type = "import_failed"
	TypeImportHeld        Type = "import_held_for_review"
	TypeDriftDetected     Type = "drift_detected"
	TypeDriftConfirmed    Type = "drift_confirmed"
	TypeSearched          Type = "searched"
	TypeAdded             Type = "added"
	TypeFileRenamed       Type = "file_renamed"
	TypeFileRemoved       Type = "file_removed"
	TypeReidentified      Type = "reidentified"
	TypeMetadataRefreshed Type = "metadata_refreshed"
	TypeMonitoringChanged Type = "monitoring_changed"
	TypeRequestApproved   Type = "request_approved"

	TypeTranscodeCompleted Type = "transcode_completed"
	TypeTranscodeFailed    Type = "transcode_failed"
	TypeTranscodeRejected  Type = "transcode_rejected"
)

func (t Type) Valid() bool {
	switch t {
	case TypeGrabbed, TypeDownloadCompleted, TypeDownloadFailed,
		TypeDownloadCancelled, TypeGrabWidened,
		TypeImported, TypeImportFailed, TypeImportHeld,
		TypeDriftDetected, TypeDriftConfirmed,
		TypeSearched,
		TypeAdded, TypeFileRenamed, TypeFileRemoved,
		TypeReidentified, TypeMetadataRefreshed, TypeMonitoringChanged,
		TypeRequestApproved,
		TypeTranscodeCompleted, TypeTranscodeFailed, TypeTranscodeRejected:
		return true
	}
	return false
}
