package evaluator

type AdmissionRequest struct {
	Fingerprint string `json:"fingerprint"`
	ImageDigest string `json:"image_digest"`
	Registry    string `json:"registry"`
	Labels      map[string]string `json:"labels"`
}

type Decision struct {
	Allowed       bool
	Reason        string
	PolicyVersion string
}
