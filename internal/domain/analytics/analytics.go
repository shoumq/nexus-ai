package analytics

import (
	"fmt"
	"math"
	"nexus/internal/dto"
	"sort"
	"strings"
	"time"
)

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

func ComputeEnergyByHour(pts []dto.TrackPoint) map[int]float64 {
	sum := make(map[int]float64)
	cnt := make(map[int]float64)

	for _, p := range pts {
		h := p.TS.Hour()
		e := energyScore(p.SleepHours, p.Mood, p.Activity)
		sum[h] += e
		cnt[h]++
	}

	out := make(map[int]float64, len(cnt))
	for h, c := range cnt {
		if c <= 0 {
			continue
		}
		out[h] = round2(sum[h] / c)
	}

	out = smoothObservedHours(out, 2)

	return out
}

func ComputeEnergyByWeekday(pts []dto.TrackPoint) map[string]float64 {
	daySum := map[time.Weekday]float64{}
	dayCnt := map[time.Weekday]float64{}

	for _, p := range pts {
		d := p.TS.Weekday()
		e := energyScore(p.SleepHours, p.Mood, p.Activity)
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

func ComputeProductivityModel(pts []dto.TrackPoint, energyByHour map[int]float64, c dto.Constraints) dto.ProductivityModel {
	weights := map[string]float64{
		"energy_mean":   0.55,
		"energy_stable": 0.20,
		"sleep_ok":      0.15,
		"mood_ok":       0.10,
	}

	meanEnergy := meanMap(energyByHour)
	stability := 100 - stdMap(energyByHour)
	sleepOK := percentSleepInRange(pts, 7.0, 9.0)
	moodOK := percentMoodAbove(pts, 6.5)

	score := weights["energy_mean"]*meanEnergy +
		weights["energy_stable"]*stability +
		weights["sleep_ok"]*sleepOK +
		weights["mood_ok"]*moodOK

	if c.WorkStartHour >= 0 && c.WorkEndHour > c.WorkStartHour {
		wh := meanHourRange(energyByHour, c.WorkStartHour, c.WorkEndHour)
		score = 0.7*score + 0.3*wh
	}

	return dto.ProductivityModel{
		Weights: weights,
		Score:   round2(clamp(score, 0, 100)),
	}
}

func ComputeBurnoutRisk(pts []dto.TrackPoint, model dto.ProductivityModel) dto.BurnoutRisk {
	reasons := []string{}

	sleepDebt := avgSleep(pts, 14) < 6.6
	moodDown := moodTrend(pts, 14) < -0.15
	energyVolatile := energyVolatility(pts, 14) > 18.0
	lowProd := model.Score < 45

	score := 0.0
	if sleepDebt {
		score += 30
		reasons = append(reasons, "Накопление недосыпа за последние ~2 недели")
	}
	if moodDown {
		score += 25
		reasons = append(reasons, "Нисходящий тренд настроения за последние ~2 недели")
	}
	if energyVolatile {
		score += 20
		reasons = append(reasons, "Высокая волатильность энергии (резкие скачки)")
	}
	if lowProd {
		score += 25
		reasons = append(reasons, "Низкий интегральный показатель продуктивности")
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

func ComputeOptimalSchedule(energyByHour map[int]float64, pts []dto.TrackPoint) dto.OptimalSchedule {
	wins := make([]dto.Win, 0, len(energyByHour))

	for h := 0; h < 24; h++ {
		v1, ok1 := energyByHour[h]
		v2, ok2 := energyByHour[(h+1)%24]
		if !ok1 || !ok2 {
			continue // no data for at least one hour, skip window
		}
		v := (v1 + v2) / 2
		wins = append(wins, dto.Win{Start: h, Val: v})
	}

	sort.Slice(wins, func(i, j int) bool { return wins[i].Val > wins[j].Val })

	best := uniqueWindows(wins, 3, 2)
	if len(best) == 0 {
		best = bestSingleHours(energyByHour, 3)
	}
	light := uniqueWindows(wins, 2, 2, best...)
	if len(light) == 0 {
		light = bestSingleHours(energyByHour, 2)
	}

	sleepWindow := inferSleepWindow(pts)

	return dto.OptimalSchedule{
		SuggestedSleepWindow: sleepWindow,
		BestFocusHours:       best,
		BestLightTasksHours:  light,
		RecoveryTips: []string{
			"Планируй сложные задачи на пики энергии, рутину — на средние окна.",
			"Если энергия падает после обеда — попробуй прогулку 10–15 минут.",
			"2–3 раза в неделю делай фокус-блок 60–90 минут без встреч.",
		},
	}
}

func ObservedHoursList(m map[int]float64) string {
	hs := make([]int, 0, len(m))
	for h := range m {
		hs = append(hs, h)
	}
	sort.Ints(hs)
	parts := make([]string, 0, len(hs))
	for _, h := range hs {
		parts = append(parts, fmt.Sprintf("%02d:00", h))
	}
	return strings.Join(parts, ", ")
}

func smoothObservedHours(m map[int]float64, radius int) map[int]float64 {
	if len(m) == 0 {
		return m
	}
	out := make(map[int]float64, len(m))
	for h := range m {
		var s float64
		var c float64
		for k := -radius; k <= radius; k++ {
			hh := (h + k + 24) % 24
			v, ok := m[hh]
			if !ok {
				continue
			}
			s += v
			c++
		}
		if c == 0 {
			out[h] = m[h]
		} else {
			out[h] = round2(s / c)
		}
	}
	return out
}

func energyScore(sleepHours, mood, activity float64) float64 {
	sleepComponent := 100 * math.Exp(-math.Pow((sleepHours-7.75)/2.0, 2))
	moodComponent := clamp01(mood/10.0) * 100
	actComponent := clamp01(activity/10.0) * 100

	e := 0.45*sleepComponent + 0.35*moodComponent + 0.20*actComponent
	return clamp(e, 0, 100)
}

func bestSingleHours(energyByHour map[int]float64, k int) []string {
	arr := make([]dto.Kv, 0, len(energyByHour))
	for h, v := range energyByHour {
		arr = append(arr, dto.Kv{K: h, V: v})
	}
	sort.Slice(arr, func(i, j int) bool { return arr[i].V > arr[j].V })
	if len(arr) > k {
		arr = arr[:k]
	}

	out := make([]string, 0, len(arr))
	for _, it := range arr {
		out = append(out, fmt.Sprintf("%02d:00–%02d:00", it.K, (it.K+1)%24))
	}
	return out
}

func uniqueWindows(wins []dto.Win, n int, length int, avoid ...string) []string {
	avoidSet := map[string]bool{}
	for _, a := range avoid {
		avoidSet[a] = true
	}

	out := []string{}
	used := make([]bool, 24)

	mark := func(start int) {
		for i := 0; i < length; i++ {
			used[(start+i)%24] = true
		}
	}

	for _, w := range wins {
		label := fmt.Sprintf("%02d:00–%02d:00", w.Start, (w.Start+length)%24)
		if avoidSet[label] {
			continue
		}

		ok := true
		for i := 0; i < length; i++ {
			if used[(w.Start+i)%24] {
				ok = false
				break
			}
		}
		if !ok {
			continue
		}

		out = append(out, label)
		mark(w.Start)

		if len(out) == n {
			break
		}
	}
	return out
}

func inferSleepWindow(pts []dto.TrackPoint) string {
	avg := avgSleep(pts, 14)
	wakeHour := 7.5
	bed := wakeHour - avg
	for bed < 0 {
		bed += 24
	}
	return fmt.Sprintf("%s–%s", fmtHHMM(bed), fmtHHMM(wakeHour))
}

func fmtHHMM(h float64) string {
	hh := int(math.Floor(h)) % 24
	mm := int(math.Round((h - math.Floor(h)) * 60))
	if mm == 60 {
		mm = 0
		hh = (hh + 1) % 24
	}
	return fmt.Sprintf("%02d:%02d", hh, mm)
}

func clamp01(x float64) float64 { return clamp(x, 0, 1) }

func clamp(x, a, b float64) float64 {
	if x < a {
		return a
	}
	if x > b {
		return b
	}
	return x
}

func round2(x float64) float64 {
	return math.Round(x*100) / 100
}

func meanMap(m map[int]float64) float64 {
	var s float64
	var c float64
	for _, v := range m {
		s += v
		c++
	}
	if c == 0 {
		return 0
	}
	return s / c
}

func stdMap(m map[int]float64) float64 {
	mean := meanMap(m)
	var s float64
	var c float64
	for _, v := range m {
		d := v - mean
		s += d * d
		c++
	}
	if c == 0 {
		return 0
	}
	return math.Sqrt(s / c)
}

func percentSleepInRange(pts []dto.TrackPoint, lo, hi float64) float64 {
	var ok float64
	for _, p := range pts {
		if p.SleepHours >= lo && p.SleepHours <= hi {
			ok++
		}
	}
	return 100 * ok / float64(len(pts))
}

func percentMoodAbove(pts []dto.TrackPoint, thr float64) float64 {
	var ok float64
	for _, p := range pts {
		if p.Mood >= thr {
			ok++
		}
	}
	return 100 * ok / float64(len(pts))
}

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

func energyVolatility(pts []dto.TrackPoint, days int) float64 {
	cut := pts[len(pts)-1].TS.AddDate(0, 0, -days)
	var vals []float64
	for _, p := range pts {
		if p.TS.After(cut) {
			vals = append(vals, energyScore(p.SleepHours, p.Mood, p.Activity))
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

func meanHourRange(m map[int]float64, start, end int) float64 {
	if start < 0 {
		start = 0
	}
	if end > 24 {
		end = 24
	}
	if end <= start {
		return meanMap(m)
	}
	var s float64
	var c float64
	for h := start; h < end; h++ {
		s += m[h]
		c++
	}
	if c == 0 {
		return 0
	}
	return s / c
}
