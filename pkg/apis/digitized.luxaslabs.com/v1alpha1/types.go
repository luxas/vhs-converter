package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
)

const (
	ConfigKind              = "Config"
	ProjectKind             = "Project"
	CassetteKind            = "Cassette"
	ClipKind                = "Clip"
	MergeCassetteActionKind = "MergeCassetteAction"
	ListKind                = "List"
)

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
type Config struct {
	metav1.TypeMeta `json:",inline"`

	// RawStoragePath specifies the path where the "database" of the daemon is on the filesystem
	RawStoragePath string `json:"rawStoragePath"`
	// WorkInProgressPath specifies the path where "work in progress" cassettes will be stored. When the
	// cassette is merged, both the clips and the merged recording will be moved to FinishedRecordingPath.
	WorkInProgressPath string `json:"workInProgressPath"`
	// FinishedRecordingPath specifies the path where fully-merged recordings are stored
	FinishedRecordingPath string `json:"finishedRecordingPath"`
	// ListenAddress specifies what host and port for the webserver and API to listen on
	ListenAddress string `json:"listenAddress"`
}

func (Config) IsNamespaced() bool { return false }

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
type Recorder struct {
	metav1.TypeMeta `json:",inline"`
	// Only here for implementing runtime.Object
	// TODO: Need to split these in some sane way
	metav1.ObjectMeta `json:"-"`

	Spec   RecorderSpec   `json:"spec"`
	Status RecorderStatus `json:"status"`
}

func (Recorder) IsNamespaced() bool { return false }
func (Recorder) IsSingleton() bool  { return true }

type RecorderSpec struct {
	// Default: None
	Action   RecorderAction `json:"action"`
	Cassette string         `json:"cassette"`
}

type RecorderStatus struct{}

type RecorderAction string

const (
	RecorderActionNone    RecorderAction = "None"
	RecorderActionPreview RecorderAction = "Preview"
	RecorderActionRecord  RecorderAction = "Record"
	// Can merging be done in the background maybe?
	// RecorderActionMerge RecorderAction = "Merge"
)

// Project represents a project for multiple Cassettes.
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
type Project struct {
	metav1.TypeMeta `json:",inline"`
	// .name is user-defined, and used in this struct as the Project name
	// .name must be a DNS label
	// Project is not namespaced, hence .namespace==""
	metav1.ObjectMeta `json:"metadata"`

	Spec   ProjectSpec   `json:"spec"`
	Status ProjectStatus `json:"status"`
}

func (Project) IsNamespaced() bool { return false }

type ProjectSpec struct {
	Client ClientInfo `json:"client"`
	// Price in euros
	Price          uint16 `json:"price"`
	ShouldFinalize bool   `json:"shouldFinalize"`
}

type ClientInfo struct {
	Name      string `json:"name"`
	Email     string `json:"email"`
	Telephone string `json:"telephone"`
}

type ProjectStatus struct {
	// Stats here, e.g. avg, stdev & CI for length & kbps
	IsFinished bool `json:"isFinished"`
}

// Cassette represents a tape cassette, belonging to a Project, but containing many Clips
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
type Cassette struct {
	metav1.TypeMeta `json:",inline"`
	// .name is a "%3d"-formatted uint16, from "001" to "999"
	// .namespace is the Project name the Clip and Cassette belongs to
	metav1.ObjectMeta `json:"metadata"`

	Spec   CassetteSpec   `json:"spec"`
	Status CassetteStatus `json:"status"`
}

func (Cassette) IsNamespaced() bool { return true }

type CassetteSpec struct {
	// Mandatory fields
	Description string       `json:"description"`
	Type        CassetteType `json:"type"`
	MaxPlayTime uint16       `json:"maxPlayTime"`

	// Level-triggered flags
	// They are all mutually exclusive
	ShouldMerge bool `json:"shouldMerge"`
	//ShouldRecord  bool `json:"shouldRecord"`
	//ShouldPreview bool `json:"shouldPreview"`

	// Optional fields
	Tags           []string    `json:"tags,omitempty"`
	VideoStartDate metav1.Time `json:"videoStartDate,omitempty"`
	VideoEndDate   metav1.Time `json:"videoEndDate,omitempty"`
}

type CassetteType string

const (
	CassetteTypeVideo8 CassetteType = "Video8"
	CassetteTypeHi8    CassetteType = "Hi8"
	CassetteTypeVHS    CassetteType = "VHS"
	CassetteTypeVHSC   CassetteType = "VHS-C"
)

type CassetteStatus struct {
	// Stats here, e.g. avg, stdev & CI for kbps & MB and total for MB & length
	IsMerged bool `json:"isMerged"`
}

// Clip represents one individual clip between 0-10 minutes.
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
type Clip struct {
	metav1.TypeMeta `json:",inline"`
	// .name is a "%3d-%3d"-formatted uint16, each format directive valid from "001" to "999"
	// The first "%3d" term is the index/name of the Cassette this Clip belongs to, and the other one is
	// the index of the Clip itself
	// .namespace is the Project name the Clip and Cassette belongs to
	metav1.ObjectMeta `json:"metadata"`

	Spec   ClipSpec   `json:"spec"`
	Status ClipStatus `json:"status"`
}

func (Clip) IsNamespaced() bool { return true }

type ClipSpec struct{}

type ClipStatus struct {
	Kbps  uint64 `json:"kbps"`
	Bytes uint64 `json:"bytes"`
	// Seconds
	Length     uint64      `json:"length"`
	RecordDate metav1.Time `json:"recordDate"`
}

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
type MergeCassetteAction struct {
	metav1.TypeMeta `json:",inline"`
	// not used, only placeholder for implementing libgitops runtime.Object
	metav1.ObjectMeta `json:"-"`
	// All videoclip sections that should be merged together.
	Sections []MergeSection `json:"sections"`
}

type MergeSection struct {
	FromVideo    uint16          `json:"fromVideo"`
	FromDuration metav1.Duration `json:"fromDuration"`
	ToVideo      uint16          `json:"toVideo"`
	ToDuration   metav1.Duration `json:"toDuration"`
}

func (MergeCassetteAction) IsNamespaced() bool { return false }

// TODO: Temporary hack. Need to find a way to make both ListMeta and ObjectMeta fit in under
// the same umbrella.
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
type List struct {
	metav1.TypeMeta `json:",inline"`
	// not used, only placeholder for implementing libgitops runtime.Object
	metav1.ObjectMeta `json:"-"`

	Items []runtime.Object `json:"items"`
}

func (List) IsNamespaced() bool { return false }
