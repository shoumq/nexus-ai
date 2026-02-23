package hepler

import (
	"encoding/json"
	"fmt"
	"nexus/internal/dto"
	"sort"
	"strings"
)

const SystemPromptRU = `Ты — строгий аналитик данных о привычках, энергии, продуктивности и риске выгорания. Твоя задача — написать короткий практичный разбор на русском языке, используя ТОЛЬКО факты из входных данных. Обращайся к человеку на "ты" (не используй "пользователь", пиши "у тебя", "ты").

КРИТИЧНЫЕ ПРАВИЛА
1) Выводи ТОЛЬКО чистый текст. Никакого Markdown: не используй **, __, *, _, ` + "`" + `, #, списки с '-' или '•', и нумерацию '1.'.
2) Запрещены служебные блоки и размышления: не используй '<think>', '</think>', 'analysis', 'thoughts'.
3) Используй только наблюдаемые часы и дни недели из входных данных. Отсутствие данных НЕ означает низкое значение.
4) Разрешено использовать user_notes как контекст. Можно делать аккуратные причинные выводы, если они явно указаны в заметках пользователя. Не придумывай новые причины.
5) Если user_notes не пустой — ОБЯЗАТЕЛЬНО упомяни заметки в одном предложении с префиксом "Заметки:" в блоке "Энергия" или "Выгорание". Не искажай текст заметок.
6) Не делай медицинских заявлений и диагнозов. Формулировки должны быть осторожные: "может снижать", "могло повлиять", "вероятно связано с".
7) Запрещено придумывать тренды/стабильность/падения/рост/циклы, если num_points < 5 ИЛИ num_observed_hours < 5 ИЛИ num_observed_days < 5. В этом случае можно только перечислять наблюдаемые значения и сказать, что данных мало.
8) Если num_points >= 5 И num_observed_hours >= 5 И num_observed_days >= 5 — ЗАПРЕЩЕНО писать "Данных мало" и "вывод предварительный".
9) Запрещено делать выводы про периоды, по которым нет наблюдений.
10) Запрещено называть значение 'низким', если оно > 60/100. Разрешённые формулировки: 'высокий', 'умеренный', 'ниже, чем', 'чуть ниже'.
11) Если burnout_level = 'unknown', ты ОБЯЗАН вставить дословно фразу:
'Риск выгорания пока неизвестен из-за недостатка данных.'
И ты НЕ имеешь права называть риск низким/средним/высоким или добавлять оценки/проценты риска.
12) Не противоречь входным цифрам. Не меняй часы, дни недели и значения.
13) Если наблюдаемый день недели всего один — нельзя писать 'лучший/худший день'. Можно только: 'Есть данные только за <день>.'

ФОРМАТ ОТВЕТА (СТРОГО)
Ответ состоит ровно из 4 блоков в указанном порядке. Каждый блок начинается с отдельной строки-заголовка БЕЗ двоеточия:
Энергия
Выгорание
Что делать завтра
Что добавить в трекинг

После заголовка блока — 2–5 коротких предложений. Между блоками — одна пустая строка.

СОДЕРЖАНИЕ БЛОКОВ
Энергия: 1–2 пика (час + значение), при необходимости 1–2 минимальных среди наблюдаемых (час + значение). Про дни недели: если >=2 — сравнить; если 1 — констатация.
Выгорание: если unknown — обязательная фраза; иначе уровень (low/medium/high) + 1–2 причины из reasons.
Что делать завтра: ровно 3 конкретных действия, привязанных к best_focus_hours / best_light_tasks_hours или наблюдаемым часам.
Что добавить в трекинг: 2–3 метрики ТОЛЬКО из списка: время сна/подъёма, кофеин, стресс, дневной план/нагрузка, встречи, питание, субъективная усталость, качество сна, концентрация.

ПРОВЕРКА ПЕРЕД ОТВЕТОМ (СДЕЛАЙ МОЛЧА)
- 4 блока есть
- в "Что делать завтра" ровно 3 действия
- если burnout_level unknown — обязательная фраза есть дословно`

const ContinuePromptTmplRU = `Продолжи ответ с места, где оборвалось. Не повторяй уже написанное.
Выведи только продолжение чистым текстом на русском.
Соблюдай все правила из system prompt, включая формат 4 блоков.
Текст, который уже отправлен пользователю:
%s`

const RepairPromptTmplRU = `Исправь ответ так, чтобы он строго соответствовал правилам system prompt и строго формату 4 блоков.
Не добавляй новых фактов и новых часов/дней.
Требования:
- 4 блока с заголовками ровно: Энергия / Выгорание / Что делать завтра / Что добавить в трекинг
- В каждом блоке 2–5 коротких предложений
- В блоке "Что делать завтра" ровно 3 действия (3 отдельных предложения)
- Если num_points >= 5 И num_observed_hours >= 5 И num_observed_days >= 5 — нельзя писать "Данных мало" и "вывод предварительный"
- Если burnout_level = unknown — обязательно дословно: "Риск выгорания пока неизвестен из-за недостатка данных."
Верни ПОЛНЫЙ исправленный текст целиком (не продолжение).

ВХОДНЫЕ АГРЕГАТЫ:
num_points=%d
num_observed_hours=%d
num_observed_days=%d
observed_hours=%s
observed_weekdays=%s
best_focus_hours=%s
best_light_tasks_hours=%s
burnout_level=%s

ИСПРАВЛЯЕМЫЙ ТЕКСТ:
%s`

func BuildRussianPrompt(p dto.HFPrompt) string {
	topHours := topKHours(p.EnergyByHour, 4, true)
	botHours := topKHours(p.EnergyByHour, 3, false)
	topDays := topKWeekdays(p.EnergyByWeekday, 2, true)
	botDays := topKWeekdays(p.EnergyByWeekday, 2, false)

	energyByHourJSON, _ := json.Marshal(p.EnergyByHour)
	energyByWeekdayJSON, _ := json.Marshal(p.EnergyByWeekday)

	s := p.ProposedSchedule

	notesBlock := ""
	if strings.TrimSpace(p.UserNotes) != "" {
		notesBlock = "\nuser_notes=\n" + p.UserNotes + "\n"
	}

	return fmt.Sprintf(
		`Агрегированные метрики пользователя. Важно: отсутствие данных НЕ означает низкую энергию.

num_points=%d
num_observed_hours=%d
observed_hours=%s
energy_by_hour_json=%s
top_hours=%s
bottom_hours=%s

num_observed_days=%d
observed_weekdays_full=%s
energy_by_weekday_json=%s
top_weekdays=%s
bottom_weekdays=%s
%s
productivity_score=%.2f
burnout_score=%.2f
burnout_level=%s
burnout_reasons=%s

suggested_sleep_window=%s
best_focus_hours=%s
best_light_tasks_hours=%s
recovery_tips=%s

Сделай ответ строго по правилам system prompt и строго в формате 4 блоков.`,
		p.NumPoints,
		p.NumObservedHours,
		p.ObservedHoursList,
		string(energyByHourJSON),
		strings.Join(topHours, ", "),
		strings.Join(botHours, ", "),

		p.NumObservedWeekdays,
		p.ObservedWeekdaysList,
		string(energyByWeekdayJSON),
		strings.Join(topDays, ", "),
		strings.Join(botDays, ", "),
		notesBlock,

		p.ProductivityScore,
		p.BurnoutScore,
		p.BurnoutLevel,
		strings.Join(p.BurnoutReasons, "; "),

		s.SuggestedSleepWindow,
		strings.Join(s.BestFocusHours, ", "),
		strings.Join(s.BestLightTasksHours, ", "),
		strings.Join(s.RecoveryTips, " | "),
	)
}

func topKHours(m map[int]float64, k int, desc bool) []string {
	arr := make([]dto.Kv, 0, len(m))
	for h, v := range m {
		arr = append(arr, dto.Kv{K: h, V: v})
	}
	sort.Slice(arr, func(i, j int) bool {
		if desc {
			return arr[i].V > arr[j].V
		}
		return arr[i].V < arr[j].V
	})
	if len(arr) > k {
		arr = arr[:k]
	}
	out := make([]string, 0, len(arr))
	for _, it := range arr {
		out = append(out, fmt.Sprintf("%02d:00 (%.1f)", it.K, it.K))
	}
	return out
}

func topKWeekdays(m map[string]float64, k int, desc bool) []string {
	arr := make([]dto.Kvs, 0, len(m))
	for d, v := range m {
		arr = append(arr, dto.Kvs{K: d, V: v})
	}
	sort.Slice(arr, func(i, j int) bool {
		if desc {
			return arr[i].V > arr[j].V
		}
		return arr[i].V < arr[j].V
	})
	if len(arr) > k {
		arr = arr[:k]
	}
	out := make([]string, 0, len(arr))
	for _, it := range arr {
		out = append(out, fmt.Sprintf("%s (%.1f)", it.V, it.V))
	}
	return out
}
