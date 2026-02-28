package dto

import "time"

// ====== INPUT/OUTPUT domain ======

type TrackPoint struct {
	TS            time.Time `json:"ts"`
	SleepHours    float64   `json:"sleep_hours"`
	SleepStart    string    `json:"sleep_start"`
	SleepEnd      string    `json:"sleep_end"`
	Mood          float64   `json:"mood"`
	Activity      float64   `json:"activity"`
	Productive    float64   `json:"productive"`
	Stress        float64   `json:"stress"`
	Energy        float64   `json:"energy"`
	Concentration float64   `json:"concentration"`
	SleepQuality  float64   `json:"sleep_quality"`
	Caffeine      bool      `json:"caffeine"`
	Alcohol       bool      `json:"alcohol"`
	Workout       bool      `json:"workout"`
	LLMText       string    `json:"llm_text"`
	AnalysisStatus string   `json:"analysis_status"`
}

type Period string

const (
	PeriodUnspecified Period = ""
	PeriodDay         Period = "day"
	PeriodWeek        Period = "week"
	PeriodMonth       Period = "month"
	PeriodAll         Period = "all"
)

type TrackRequest struct {
	UserID int32        `json:"-"`
	UserTZ string       `json:"user_tz"`
	Points []TrackPoint `json:"points"`
}

type UserProfile struct {
	UserID  int32  `json:"user_id"`
	Name    string `json:"name"`
	Email   string `json:"email"`
	Emoji   string `json:"emoji"`
	BgIndex int32  `json:"bg_index"`
	IsFriend bool  `json:"is_friend"`
}

type FriendRequest struct {
	ID        int64       `json:"id"`
	From      UserProfile `json:"from"`
	To        UserProfile `json:"to"`
	Status    string      `json:"status"`
	CreatedAt time.Time   `json:"created_at"`
}

type AnalyzeRequest struct {
	UserID      int32       `json:"-"`
	UserTZ      string      `json:"user_tz"`
	WeekStarts  string      `json:"week_starts"`
	Constraints Constraints `json:"constraints"`
	Period      Period      `json:"period"`
}

type Constraints struct {
	WorkStartHour int `json:"work_start_hour"`
	WorkEndHour   int `json:"work_end_hour"`
}

type AnalyzeResponse struct {
	EnergyByWeekday   map[string]float64 `json:"energy_by_weekday"`
	ProductivityModel ProductivityModel  `json:"productivity_model"`
	BurnoutRisk       BurnoutRisk        `json:"burnout_risk"`
	OptimalSchedule   OptimalSchedule    `json:"optimal_schedule"`
	LLMInsight        string             `json:"llm_insight"`
	Debug             map[string]any     `json:"debug,omitempty"`
}

type ProductivityModel struct {
	Weights map[string]float64 `json:"weights"`
	Score   float64            `json:"score"`
}

type BurnoutRisk struct {
	Score                 float64  `json:"score"`
	Level                 string   `json:"level"`
	Reasons               []string `json:"reasons"`
	PredictionHorizonDays int      `json:"prediction_horizon_days"`
}

type OptimalSchedule struct {
	SuggestedSleepWindow string   `json:"suggested_sleep_window"`
	BestFocusHours       []string `json:"best_focus_hours"`
	BestLightTasksHours  []string `json:"best_light_tasks_hours"`
	RecoveryTips         []string `json:"recovery_tips"`
}

// ====== scheduling helper ======

type Win struct {
	Start int
	Val   float64
}

// ====== AI (LLM) prompt/data ======

type AIPrompt struct {
	UserTZ               string
	Period               Period
	PeriodStart          time.Time
	PeriodEnd            time.Time
	EnergyByWeekday      map[string]float64
	ProductivityScore    float64
	BurnoutScore         float64
	BurnoutLevel         string
	BurnoutReasons       []string
	NumPoints            int
	NumObservedWeekdays  int
	NumObservedDays      int
	ObservedWeekdaysList string
	UserNotes            string
	AvgSleepHours        float64
	AvgSleepQuality      float64
	AvgMood              float64
	AvgActivity          float64
	AvgProductive        float64
	AvgStress            float64
	AvgEnergy            float64
	AvgConcentration     float64
	AvgSleepStart        string
	AvgSleepEnd          string
	MinEnergy            float64
	MaxEnergy            float64
	MinStress            float64
	MaxStress            float64
	MinSleepHours        float64
	MaxSleepHours        float64
}

// ====== AI chat API payloads ======

type AIChatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type AIChatRequest struct {
	Model       string          `json:"model"`
	Messages    []AIChatMessage `json:"messages"`
	MaxTokens   int             `json:"max_tokens,omitempty"`
	Temperature float64         `json:"temperature,omitempty"`
	TopP        float64         `json:"top_p,omitempty"`
	Stream      bool            `json:"stream,omitempty"`
}

// В твоём коде это было анонимными struct{} внутри Choices.
// Чтобы "все структуры" были явными — выносим:

type AIChatChoiceMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type AIChatChoice struct {
	Message      AIChatChoiceMessage `json:"message"`
	FinishReason string              `json:"finish_reason"`
}

type AIChatResponse struct {
	Choices []AIChatChoice `json:"choices"`
	Error   any            `json:"error,omitempty"`
}

// Остальные AI типы (сейчас не используются в твоём фрагменте, но у тебя объявлены):

type AIRequest struct {
	Inputs     string         `json:"inputs"`
	Parameters map[string]any `json:"parameters,omitempty"`
	Options    map[string]any `json:"options,omitempty"`
}

type AITextGenItem struct {
	GeneratedText string `json:"generated_text"`
}

// ====== local helper structs ======

// В helper-ах у тебя был локальный type kv, но это тоже структура.
// Явно вынесем (чтобы "все структуры"):

type Kv struct {
	K int
	V float64
}

// Аналогично kvs был локальным типом внутри topKWeekdays.
// Можно вынести если хочешь 100% "всё":

type Kvs struct {
	K string
	V float64
}
