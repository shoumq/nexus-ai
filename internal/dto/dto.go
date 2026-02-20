package dto

import "time"

// ====== INPUT/OUTPUT domain ======

type TrackPoint struct {
	TS         time.Time `json:"ts"`
	SleepHours float64   `json:"sleep_hours"`
	Mood       float64   `json:"mood"`
	Activity   float64   `json:"activity"`
	Productive float64   `json:"productive"`
}

type AnalyzeRequest struct {
	UserTZ      string       `json:"user_tz"`
	Points      []TrackPoint `json:"points"`
	WeekStarts  string       `json:"week_starts"`
	Constraints Constraints  `json:"constraints"`
}

type Constraints struct {
	WorkStartHour int `json:"work_start_hour"`
	WorkEndHour   int `json:"work_end_hour"`
}

type AnalyzeResponse struct {
	EnergyByHour      map[int]float64    `json:"energy_by_hour"`
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

// ====== HF (LLM) prompt/data ======

type HFPrompt struct {
	UserTZ               string
	EnergyByHour         map[int]float64
	EnergyByWeekday      map[string]float64
	ProductivityScore    float64
	BurnoutScore         float64
	BurnoutLevel         string
	BurnoutReasons       []string
	ProposedSchedule     OptimalSchedule
	NumPoints            int
	NumObservedHours     int
	NumObservedWeekdays  int
	ObservedHoursList    string
	ObservedWeekdaysList string
}

// ====== HF chat API payloads ======

type HfChatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type HfChatRequest struct {
	Model       string          `json:"model"`
	Messages    []HfChatMessage `json:"messages"`
	MaxTokens   int             `json:"max_tokens,omitempty"`
	Temperature float64         `json:"temperature,omitempty"`
	TopP        float64         `json:"top_p,omitempty"`
	Stream      bool            `json:"stream,omitempty"`
}

// В твоём коде это было анонимными struct{} внутри Choices.
// Чтобы "все структуры" были явными — выносим:

type HfChatChoiceMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type HfChatChoice struct {
	Message      HfChatChoiceMessage `json:"message"`
	FinishReason string              `json:"finish_reason"`
}

type HfChatResponse struct {
	Choices []HfChatChoice `json:"choices"`
	Error   any            `json:"error,omitempty"`
}

// Остальные HF типы (сейчас не используются в твоём фрагменте, но у тебя объявлены):

type HfRequest struct {
	Inputs     string         `json:"inputs"`
	Parameters map[string]any `json:"parameters,omitempty"`
	Options    map[string]any `json:"options,omitempty"`
}

type HfTextGenItem struct {
	GeneratedText string `json:"generated_text"`
}

// ====== local helper structs ======

// В build/topKHours у тебя локальный type kv, но это тоже структура.
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
