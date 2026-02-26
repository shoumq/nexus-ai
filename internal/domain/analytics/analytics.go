package analytics

import (
	"math"
	"nexus/internal/dto"
	"sort"
	"strings"
	"time"
)

// ObservedWeekdaysList возвращает отсортированный список ключей (дней) в формате "Mon, Tue".
// Пример: ObservedWeekdaysList(map[string]float64{"Mon": 1, "Wed": 2}) -> "Mon, Wed".
func ObservedWeekdaysList(m map[string]float64) string {
	if len(m) == 0 {
		return ""
	}
	keys := make([]string, 0, len(m))
	for d := range m {
		keys = append(keys, d)
	}
	sort.Strings(keys)
	return strings.Join(keys, ", ")
}

// ComputeEnergyByWeekday считает среднюю энергию по дням недели (Mon, Tue и т.д.).
// Пример: ComputeEnergyByWeekday(points)["Mon"] -> 63.2.
func ComputeEnergyByWeekday(pts []dto.TrackPoint) map[string]float64 {
	daySum := map[time.Weekday]float64{}
	dayCnt := map[time.Weekday]float64{}

	for _, p := range pts {
		d := p.TS.Weekday()
		e := energyScore(p)
		daySum[d] += e
		dayCnt[d]++
	}

	out := make(map[string]float64, len(dayCnt))
	for d, c := range dayCnt {
		if c <= 0 {
			continue
		}
		out[d.String()[:3]] = round2(daySum[d] / c)
	}
	return out
}

// ComputeProductivityModel строит интегральную модель продуктивности по дневным данным.
// Пример: ComputeProductivityModel(points).Score -> 72.4.
func ComputeProductivityModel(pts []dto.TrackPoint) dto.ProductivityModel {
	weights := map[string]float64{
		"energy_mean":    0.40,
		"energy_stable":  0.15,
		"sleep_ok":       0.10,
		"mood_ok":        0.10,
		"sleep_quality":  0.08,
		"focus_ok":       0.07,
		"stress_ok":      0.05,
		"self_energy_ok": 0.05,
	}

	meanEnergy := meanEnergyScore(pts)
	stability := 100 - stdEnergyScore(pts)
	sleepOK := percentSleepInRange(pts, 7.0, 9.0)
	moodOK := percentMoodAbove(pts, 6.5)
	sleepQualityOK := percentFieldAbove(pts, func(p dto.TrackPoint) float64 { return p.SleepQuality }, 6.5)
	focusOK := percentFieldAbove(pts, func(p dto.TrackPoint) float64 { return p.Concentration }, 6.0)
	stressOK := percentFieldBelow(pts, func(p dto.TrackPoint) float64 { return p.Stress }, 5.5)
	selfEnergyOK := percentFieldAbove(pts, func(p dto.TrackPoint) float64 { return p.Energy }, 6.0)

	score := weights["energy_mean"]*meanEnergy +
		weights["energy_stable"]*stability +
		weights["sleep_ok"]*sleepOK +
		weights["mood_ok"]*moodOK +
		weights["sleep_quality"]*sleepQualityOK +
		weights["focus_ok"]*focusOK +
		weights["stress_ok"]*stressOK +
		weights["self_energy_ok"]*selfEnergyOK

	return dto.ProductivityModel{
		Weights: weights,
		Score:   round2(clamp(score, 0, 100)),
	}
}

// ComputeBurnoutRisk оценивает риск выгорания по трендам сна/настроения/стресса и модели продуктивности.
// Пример: ComputeBurnoutRisk(points, model).Level -> "medium".
func ComputeBurnoutRisk(pts []dto.TrackPoint, model dto.ProductivityModel) dto.BurnoutRisk {
	reasons := []string{}

	sleepDebt := avgSleep(pts, 14) < 6.6
	moodDown := moodTrend(pts, 14) < -0.15
	energyVolatile := energyVolatility(pts, 14) > 18.0
	lowProd := model.Score < 45
	highStress := avgField(pts, func(p dto.TrackPoint) float64 { return p.Stress }) > 6.5
	lowSelfEnergy := avgField(pts, func(p dto.TrackPoint) float64 { return p.Energy }) < 4.5
	poorSleepQuality := avgField(pts, func(p dto.TrackPoint) float64 { return p.SleepQuality }) < 6.0
	alcoholOften := percentBool(pts, func(p dto.TrackPoint) bool { return p.Alcohol }) > 30
	workoutRare := percentBool(pts, func(p dto.TrackPoint) bool { return p.Workout }) < 20

	score := 0.0
	if sleepDebt {
		score += 25
		reasons = append(reasons, "Накопление недосыпа за последние ~2 недели")
	}
	if moodDown {
		score += 20
		reasons = append(reasons, "Нисходящий тренд настроения за последние ~2 недели")
	}
	if energyVolatile {
		score += 15
		reasons = append(reasons, "Высокая волатильность энергии (резкие скачки)")
	}
	if lowProd {
		score += 20
		reasons = append(reasons, "Низкий интегральный показатель продуктивности")
	}
	if highStress {
		score += 20
		reasons = append(reasons, "Высокий уровень стресса по самооценке")
	}
	if lowSelfEnergy {
		score += 15
		reasons = append(reasons, "Низкая самооценка энергии")
	}
	if poorSleepQuality {
		score += 10
		reasons = append(reasons, "Низкое качество сна в среднем")
	}
	if alcoholOften {
		score += 10
		reasons = append(reasons, "Частые отметки алкоголя")
	}
	if workoutRare {
		score += 5
		reasons = append(reasons, "Низкая регулярность тренировок")
	}

	score = clamp(score, 0, 100)
	level := "low"
	switch {
	case score >= 70:
		level = "high"
	case score >= 40:
		level = "medium"
	}

	if len(reasons) == 0 {
		reasons = append(reasons, "Явных триггеров выгорания не найдено по текущим данным")
	}

	return dto.BurnoutRisk{
		Score:                 round2(score),
		Level:                 level,
		Reasons:               reasons,
		PredictionHorizonDays: 14,
	}
}

// energyScore рассчитывает итоговый энергетический скор по показателям сна, настроения и активности.
// Пример: energyScore(point) -> 71.3.
func energyScore(p dto.TrackPoint) float64 {
	sleepComponent := 100 * math.Exp(-math.Pow((p.SleepHours-7.75)/2.0, 2))
	sleepQuality := clamp01(p.SleepQuality/10.0) * 100
	moodComponent := clamp01(p.Mood/10.0) * 100
	actComponent := clamp01(p.Activity/10.0) * 100
	energySelf := clamp01(p.Energy/10.0) * 100
	focusComponent := clamp01(p.Concentration/10.0) * 100

	e := 0.32*sleepComponent +
		0.13*sleepQuality +
		0.20*moodComponent +
		0.12*actComponent +
		0.18*energySelf +
		0.05*focusComponent

	if p.Caffeine {
		e += 2.5
	}
	if p.Alcohol {
		e -= 4.0
	}
	if p.Workout {
		e += 1.5
	}
	return clamp(e, 0, 100)
}

// clamp01 ограничивает значение диапазоном [0, 1].
// Пример: clamp01(1.7) -> 1.
func clamp01(x float64) float64 { return clamp(x, 0, 1) }

// clamp ограничивает значение диапазоном [a, b].
// Пример: clamp(12, 0, 10) -> 10.
func clamp(x, a, b float64) float64 {
	if x < a {
		return a
	}
	if x > b {
		return b
	}
	return x
}

// round2 округляет число до 2 знаков после запятой.
// Пример: round2(1.2345) -> 1.23.
func round2(x float64) float64 {
	return math.Round(x*100) / 100
}

// percentSleepInRange считает процент дней, где сон в диапазоне [lo, hi].
// Пример: percentSleepInRange(points, 7, 9) -> 65.
func percentSleepInRange(pts []dto.TrackPoint, lo, hi float64) float64 {
	if len(pts) == 0 {
		return 0
	}
	var ok float64
	for _, p := range pts {
		if p.SleepHours >= lo && p.SleepHours <= hi {
			ok++
		}
	}
	return 100 * ok / float64(len(pts))
}

// percentMoodAbove считает процент дней, где настроение >= thr.
// Пример: percentMoodAbove(points, 6.5) -> 52.
func percentMoodAbove(pts []dto.TrackPoint, thr float64) float64 {
	if len(pts) == 0 {
		return 0
	}
	var ok float64
	for _, p := range pts {
		if p.Mood >= thr {
			ok++
		}
	}
	return 100 * ok / float64(len(pts))
}

// percentFieldAbove считает процент дней, где поле f >= thr.
// Пример: percentFieldAbove(points, func(p) p.Concentration, 6) -> 48.
func percentFieldAbove(pts []dto.TrackPoint, f func(dto.TrackPoint) float64, thr float64) float64 {
	if len(pts) == 0 {
		return 0
	}
	var ok float64
	for _, p := range pts {
		if f(p) >= thr {
			ok++
		}
	}
	return 100 * ok / float64(len(pts))
}

// percentFieldBelow считает процент дней, где поле f <= thr.
// Пример: percentFieldBelow(points, func(p) p.Stress, 5.5) -> 40.
func percentFieldBelow(pts []dto.TrackPoint, f func(dto.TrackPoint) float64, thr float64) float64 {
	if len(pts) == 0 {
		return 0
	}
	var ok float64
	for _, p := range pts {
		if f(p) <= thr {
			ok++
		}
	}
	return 100 * ok / float64(len(pts))
}

// percentBool считает процент дней, где булево условие истинно.
// Пример: percentBool(points, func(p) p.Workout) -> 25.
func percentBool(pts []dto.TrackPoint, f func(dto.TrackPoint) bool) float64 {
	if len(pts) == 0 {
		return 0
	}
	var ok float64
	for _, p := range pts {
		if f(p) {
			ok++
		}
	}
	return 100 * ok / float64(len(pts))
}

// avgSleep считает среднее количество сна за последние days дней.
// Пример: avgSleep(points, 14) -> 6.9.
func avgSleep(pts []dto.TrackPoint, days int) float64 {
	cut := pts[len(pts)-1].TS.AddDate(0, 0, -days)
	var s float64
	var c float64
	for _, p := range pts {
		if p.TS.After(cut) {
			s += p.SleepHours
			c++
		}
	}
	if c == 0 {
		return 0
	}
	return s / c
}

// AvgSleepDays возвращает среднее количество сна за последние days дней.
// Пример: AvgSleepDays(points, 14) -> 6.9.
func AvgSleepDays(pts []dto.TrackPoint, days int) float64 {
	if len(pts) == 0 || days <= 0 {
		return 0
	}
	return avgSleep(pts, days)
}

// SleepDeltaDays считает разницу среднего сна: последние days дней минус предыдущие days дней.
// Пример: SleepDeltaDays(points, 7) -> -0.4.
func SleepDeltaDays(pts []dto.TrackPoint, days int) float64 {
	if len(pts) == 0 || days <= 0 {
		return 0
	}
	end := pts[len(pts)-1].TS
	curFrom := end.AddDate(0, 0, -days)
	prevFrom := end.AddDate(0, 0, -2*days)
	prevTo := curFrom

	cur := avgSleepBetween(pts, curFrom, end)
	prev := avgSleepBetween(pts, prevFrom, prevTo)
	if cur == 0 || prev == 0 {
		return 0
	}
	return cur - prev
}

func avgSleepBetween(pts []dto.TrackPoint, from, to time.Time) float64 {
	var s float64
	var c float64
	for _, p := range pts {
		if p.TS.After(from) && !p.TS.After(to) {
			s += p.SleepHours
			c++
		}
	}
	if c == 0 {
		return 0
	}
	return s / c
}

// moodTrend оценивает тренд настроения (средняя разница половин периода).
// Пример: moodTrend(points, 14) -> -0.2.
func moodTrend(pts []dto.TrackPoint, days int) float64 {
	cut := pts[len(pts)-1].TS.AddDate(0, 0, -days)
	var arr []dto.TrackPoint
	for _, p := range pts {
		if p.TS.After(cut) {
			arr = append(arr, p)
		}
	}
	if len(arr) < 8 {
		return 0
	}
	n := len(arr)
	first := avgMood(arr[:n/2])
	last := avgMood(arr[n/2:])
	return (last - first) / float64(days)
}

// avgMood считает среднее настроение.
// Пример: avgMood(points) -> 6.7.
func avgMood(pts []dto.TrackPoint) float64 {
	var s float64
	for _, p := range pts {
		s += p.Mood
	}
	if len(pts) == 0 {
		return 0
	}
	return s / float64(len(pts))
}

// avgField считает среднее значение произвольного поля.
// Пример: avgField(points, func(p) p.Stress) -> 5.8.
func avgField(pts []dto.TrackPoint, f func(dto.TrackPoint) float64) float64 {
	if len(pts) == 0 {
		return 0
	}
	var s float64
	for _, p := range pts {
		s += f(p)
	}
	return s / float64(len(pts))
}

// meanEnergyScore считает среднюю энергию по energyScore для всех точек.
// Пример: meanEnergyScore(points) -> 67.3.
func meanEnergyScore(pts []dto.TrackPoint) float64 {
	if len(pts) == 0 {
		return 0
	}
	var s float64
	for _, p := range pts {
		s += energyScore(p)
	}
	return s / float64(len(pts))
}

// stdEnergyScore считает стандартное отклонение energyScore для всех точек.
// Пример: stdEnergyScore(points) -> 9.4.
func stdEnergyScore(pts []dto.TrackPoint) float64 {
	if len(pts) == 0 {
		return 0
	}
	mean := meanEnergyScore(pts)
	var s float64
	for _, p := range pts {
		d := energyScore(p) - mean
		s += d * d
	}
	return math.Sqrt(s / float64(len(pts)))
}

// energyVolatility оценивает волатильность энергии за последние days дней.
// Пример: energyVolatility(points, 14) -> 12.4.
func energyVolatility(pts []dto.TrackPoint, days int) float64 {
	cut := pts[len(pts)-1].TS.AddDate(0, 0, -days)
	var vals []float64
	for _, p := range pts {
		if p.TS.After(cut) {
			vals = append(vals, energyScore(p))
		}
	}
	if len(vals) < 5 {
		return 0
	}
	mean := 0.0
	for _, v := range vals {
		mean += v
	}
	mean /= float64(len(vals))
	var s float64
	for _, v := range vals {
		d := v - mean
		s += d * d
	}
	return math.Sqrt(s / float64(len(vals)))
}
