package main

import (
	"fmt"
	"strings"
	"time"
)

type User struct {
	ID                 int64
	DepartmentID       int64
	Name               string
	Email              string
	Mobile             string
	Qualification      string
	ProfessionalTitle  string
	Role               string
	IsSystemAdmin      bool
	IsTestUser         bool
	Active             bool
	MustChangePassword bool
}

func (u User) RoleName() string {
	switch u.Role {
	case "manager":
		return "部门领导"
	case "lead":
		return "专业负责人"
	default:
		return "设计师"
	}
}

type Project struct {
	ID                      int64
	Code                    string
	Name                    string
	ShortName               string
	Size                    string
	ChiefDesigner           string
	CreatorUserID           int64
	CreatorName             string
	ExecutingLeadUserID     int64
	ExecutingLeadName       string
	StartDate               string
	ExpectedEndDate         string
	IntroAddress            string
	IntroType               string
	IntroScale              string
	IntroComponents         string
	IntroFeatures           string
	Stages                  []string
	Subitems                []ProjectSubitem
	Status                  string
	IsIncomplete            bool
	Leads                   []User
	CanEdit                 bool
	CanArchive              bool
	CanDelete               bool
	CurrentRawHours         float64
	CurrentSiteHours        float64
	CurrentEffectiveHours   float64
	CurrentForecastHours    float64
	DepartmentResourceRate  float64
	DepartmentWorkShare     float64
	CurrentParticipantCount int
	HasCorrectedHours       bool
	IsTestData              bool
}

type ProjectSubitem struct {
	ID        int64
	ProjectID int64
	Name      string
	Area      float64
	Structure string
	Notes     string
	Active    bool
}

func (p Project) HasStage(stage string) bool {
	for _, current := range p.Stages {
		if current == stage {
			return true
		}
	}
	return false
}

func (p Project) StageSummary() string {
	return strings.Join(p.Stages, "、")
}

type WorkContentFields struct {
	ProjectSubitemID int64
	Subitem          string
	Area             float64
	Structure        string
	Role             string
}

func (w WorkContentFields) LegacyText() string {
	if w.Subitem == "" && w.Area == 0 && w.Structure == "" && w.Role == "" {
		return ""
	}
	text := w.Subitem
	if w.Area > 0 {
		text += "（" + fmt.Sprintf("%.1f", w.Area) + "㎡）"
	}
	if w.Structure != "" {
		text += " · " + w.Structure
	}
	if w.Role != "" {
		text += " · " + w.Role
	}
	return strings.TrimSpace(text)
}

type WorkEntry struct {
	ID               int64
	WeekEnd          string
	UserID           int64
	ProjectID        int64
	ProjectSubitemID int64
	ProjectCode      string
	ProjectName      string
	ProjectShortName string
	Hours            float64
	WorkContent      string
	WorkSubitem      string
	WorkArea         float64
	WorkStructure    string
	WorkRole         string
	WorkCategory     string
	OtherDescription string
	EndParticipation bool
}

type LoginBackground struct {
	ID        int64
	Name      string
	AssetPath string
	MimeType  string
	Size      int64
	Active    bool
	CreatedAt string
}

func (w WorkEntry) WorkFields() WorkContentFields {
	return WorkContentFields{Subitem: w.WorkSubitem, Area: w.WorkArea, Structure: w.WorkStructure, Role: w.WorkRole}
}

type ForecastEntry struct {
	TargetWeekEnd string
	ProjectID     int64
	UserID        int64
	UserName      string
	Hours         float64
	CreatedBy     int64
}

type WeekMetric struct {
	WeekEnd       string  `json:"week_end"`
	WeekLabel     string  `json:"week_label"`
	ActualHours   float64 `json:"actual_hours"`
	ForecastHours float64 `json:"forecast_hours"`
	SiteHours     float64 `json:"site_hours"`
	Available     float64 `json:"available_hours"`
	LoadRate      float64 `json:"load_rate"`
	Bias          float64 `json:"bias"`
	AdjustedHours float64 `json:"adjusted_hours"`
	HasBias       bool    `json:"has_bias"`
	HasAdjusted   bool    `json:"has_adjusted"`
}

type EmployeeMetric struct {
	UserID               int64
	Name                 string
	Role                 string
	ActualHours          float64
	ForecastHours        float64
	ProjectHours         float64
	SiteHours            float64
	OtherHours           float64
	ProjectCount         int
	ProjectActualHours   float64
	ProjectForecastHours float64
	AvailableHours       float64
	LoadRate             float64
	Bias                 float64
	AdjustedHours        float64
	AdjustedRate         float64
	CorrectionFactor     float64
	HasBias              bool
	HasAdjusted          bool
	Alert                bool
	LoadBand             string
}

type ProjectMetric struct {
	ProjectID     int64
	Code          string
	ShortName     string
	Size          string
	ActualHours   float64
	ForecastHours float64
	SiteHours     float64
	MemberCount   int
	LoadRate      float64
}

type AuditLog struct {
	ID        int64
	ActorName string
	Action    string
	Entity    string
	Detail    string
	IPAddress string
	CreatedAt time.Time
}
