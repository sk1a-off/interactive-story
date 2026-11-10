package main

import (
	"encoding/json"
	"fmt"
	"strings"

	appgen "github.com/local/interactive-ai-story/backend/internal/application/generation"
	"github.com/local/interactive-ai-story/backend/internal/ports/generationtarget"
)

type pairedCase struct {
	ID, Family, Role string
	Input            json.RawMessage
	Repeats          int
	Writer           bool
	Facts            []string
	validate         func(map[string]any, string) validation
}

func freezePaired(tc testCase, family string, repeats int, writer bool, facts []string) pairedCase {
	raw, _ := json.Marshal(tc.Input)
	return pairedCase{tc.Name, family, tc.Role, raw, repeats, writer, facts, tc.Validate}
}
func pairedCorpus() []pairedCase {
	var out []pairedCase
	for _, tc := range browserRoleSuite() {
		switch tc.Name {
		case "state_evaluator", "quest_evaluator", "choices", "turn_planner_browser", "world_browser_positive", "world_browser_negative":
			out = append(out, freezePaired(tc, tc.Name, 2, false, nil))
		}
	}
	type scene struct {
		id, place, hero, previous, action, goal string
		facts                                   []string
		rich                                    bool
	}
	scenes := []scene{
		{"writer_archive", "Архив приливов", "Лада", "Тихон показал Ладе мокрый лист Мирона. В свете настольной лампы на листе проступила схема. Он потребовал сжечь находку. Каменный проход за стеллажами начал закрываться.", "Лада притворяется, что собирается сжечь лист над пламенем лампы, чтобы проверить реакцию Тихона, но не хочет повреждать находку.", "Дать конкретный результат проверки и новое затруднение, сохранив лист.", []string{"Лист Мирона цел и находится в руках Лады.", "Лампа горит на столе.", "Лада пытается обмануть Тихона, а не уничтожить лист.", "Не выбирать за Ладу спуск в проход."}, false},
		{"writer_airlock", "Шлюз станции «Север»", "Ирина", "Ирина стояла у закрытого шлюза. Механик Павел показал датчик: за дверью нет давления. Резервный баллон уже пуст, а ручное открытие приведёт к разгерметизации отсека.", "Ирина проверяет показания и спрашивает Павла о безопасном способе осмотра, не открывая шлюз.", "Прояснить техническое препятствие и безопасный следующий выбор.", []string{"Шлюз закрыт; его нельзя открывать как решённое героиней действие.", "Резервный баллон пуст.", "Не создавать новый баллон или чудесное восстановление давления."}, true},
		{"writer_detective", "Кабинет нотариуса", "Денис", "Денис заметил на столе нотариуса конверт с чужой печатью. Елена вернулась в кабинет и положила рядом связку ключей. Она не знала, что он видел конверт.", "Денис спокойно спрашивает Елену о времени последнего визита курьера, не обвиняя её и не упоминая конверт.", "Получить наблюдаемую реакцию и конкретный след без вынужденного признания.", []string{"Елена не знает, что Денис видел конверт.", "Денис не обвиняет Елену и не упоминает конверт.", "Не выдавать мысли Елены от её лица и не гарантировать признание."}, false},
		{"writer_market", "Рынок у переправы", "Марина", "Торговец положил на прилавок карту и повторил цену: пять медных монет. Марина пересчитала кошель — в нём оставалось только три. Торговец не обещал скидку.", "Марина предлагает три монеты и спрашивает, можно ли компенсировать остаток помощью торговцу, не соглашаясь заранее на неизвестную работу.", "Разрешить предложение торга с ценой или отказом и оставить решение о работе игроку.", []string{"У Марины ровно три монеты; карта стоит пять.", "Не описывать оплату пяти монет.", "Не принимать за Марину ещё неизвестную работу."}, false},
		{"writer_berries", "Лесная тропа", "Антон", "Антон остановился возле кустов с яркими ягодами. Его проводница Нина призналась, что не знает этот вид. Вода в фляге ещё оставалась, но еды больше не было.", "Антон осматривает листья и запах ягод, не пробуя их на вкус, и спрашивает Нину о других источниках пищи.", "Дать полезные наблюдения при сохранении неопределённости и запрета на пробу.", []string{"Ни Антон, ни Нина пока не знают вид ягод.", "Антон не должен есть или пробовать ягоды.", "Не создавать ботанический справочник или запас еды."}, false},
		{"writer_storm", "Старый мост", "Ася", "Порыв ветра качнул пролёт моста. У Аси было две верёвки и один карабин. На другом берегу Виктор закрепил свой конец, но сквозь дождь она не видела состояние узла.", "Ася проверяет собственное крепление и просит Виктора показать узел, не начиная переход, пока он не подтверждён.", "Показать проверку, реакцию напарника и обоснованную неопределённость перехода.", []string{"У Аси две верёвки и один карабин.", "Она пока не начинает переход.", "Не гарантировать безопасность невидимого узла без наблюдений."}, false},
		{"writer_apology", "Кухня после ссоры", "Олег", "Катя поставила чашку в раковину и отвернулась. Олег ранее рассказал общим знакомым её личную историю без разрешения. Она попросила не оправдываться и пока не знала, хочет ли продолжать разговор.", "Олег извиняется за конкретный поступок, не оправдываясь, и даёт Кате возможность ответить или прекратить разговор.", "Развить эмоционально правдоподобную реакцию без обязательного прощения.", []string{"Причина ссоры — разглашение личной истории без разрешения.", "Извинение не гарантирует прощение.", "Не решать за Катю и Олега дальнейшие отношения."}, false},
		{"writer_desert", "Заброшенный караван-сарай", "Яна", "Яна нашла карту с размытым краем. На ней были колодец и две дороги, но расстояния не читались. Спутник Рашид никогда не бывал в этой части пустыни. До ночи оставалось мало времени.", "Яна сопоставляет карту с видимыми ориентирами и расспрашивает Рашида о признаках старой дороги, не выбирая маршрут вслепую.", "Получить частичную проверяемую информацию и сохранить выбор маршрута.", []string{"Точных расстояний на карте не видно.", "Рашид не бывал в этой части пустыни.", "Не создавать точные координаты и не выбирать за Яну дорогу."}, false},
	}
	for _, f := range scenes {
		context := map[string]any{"previousBeatText": f.previous, "location": f.place, "player": map[string]string{"name": f.hero}, "world": map[string]any{"summary": "Действуют только факты, прямо указанные в текущей сцене."}}
		if f.rich {
			var background []string
			for i := 0; i < 65; i++ {
				background = append(background, fmt.Sprintf("Запись обслуживания %03d: рабочие проверяли маркировку кабелей в соседнем отсеке станции. Эта архивная запись не подтверждает текущее давление и не содержит новых ресурсов.", i))
			}
			context["backgroundFacts"] = background
		}
		journal := []generationtarget.JournalEntry{}
		add := func(id, category, name string, quantity int) {
			journal = append(journal, generationtarget.JournalEntry{ID: id, Category: category, Name: name, Quantity: quantity, Status: "active"})
		}
		switch f.id {
		case "writer_archive":
			add("00000000-0000-4000-8000-000000009101", "item", "Мокрый лист Мирона", 1)
		case "writer_airlock":
			add("00000000-0000-4000-8000-000000009102", "item", "Пустой резервный баллон", 0)
		case "writer_market":
			add("00000000-0000-4000-8000-000000009103", "currency", "медных монет", 3)
		case "writer_storm":
			add("00000000-0000-4000-8000-000000009104", "item", "верёвка", 2)
			add("00000000-0000-4000-8000-000000009105", "item", "карабин", 1)
		case "writer_berries":
			add("00000000-0000-4000-8000-000000009106", "item", "Фляга с оставшейся водой", 1)
		case "writer_desert":
			add("00000000-0000-4000-8000-000000009107", "item", "Карта с размытым краем", 1)
		}
		input := map[string]any{"intent": f.action, "directorGoal": f.goal, "pacing": map[string]string{"transition": "continue_scene"}, "context": context, "heroJournal": journal, "quests": []any{}}
		tc := testCase{Name: f.id, Role: "writer", MaxTokens: 4096, Input: input, Validate: validateProse(f.previous, 3, 5, 220, 650)}
		out = append(out, freezePaired(tc, f.id, 3, true, f.facts))
	}
	baseJournal := []generationtarget.JournalEntry{{ID: "55555555-5555-4555-8555-555555555555", Category: "currency", Name: "медных монет", Quantity: 3, Status: "active"}, {ID: "66666666-6666-4666-8666-666666666666", Category: "item", Name: "походный паёк", Quantity: 2, Status: "active"}}
	for _, f := range []struct {
		id, action, beat string
		want             int
	}{
		{"journal_planned_purchase", "Обдумать покупку карты.", "Лада подумала купить карту за пять монет. В кошельке у неё оставалось три. Она не платила и ничего не получила.", 0},
		{"journal_metaphor", "Собраться с духом.", "Ладе показалось, будто её храбрость выросла вдвое. Это была метафора решимости, а не новая способность. Её вещи и деньги не изменились.", 0},
		{"journal_consumption", "Съесть один паёк.", "Лада полностью съела один из двух походных пайков. В сумке остался один паёк. Монет она не тратила.", 1},
	} {
		f := f
		input := map[string]any{"action": f.action, "intent": f.action, "newBeat": f.beat, "currentJournal": baseJournal, "bootstrapJournal": false, "context": map[string]any{"player": map[string]string{"name": "Лада"}}}
		validate := func(_ map[string]any, raw string) validation {
			normalized, err := appgen.CheckJournalProposal(f.beat, baseJournal, json.RawMessage(raw))
			var changes []map[string]any
			parseErr := json.Unmarshal(normalized, &changes)
			ok := err == nil && parseErr == nil && len(changes) == f.want
			if f.want == 1 && ok {
				ok = changes[0]["entryId"] == "66666666-6666-4666-8666-666666666666" && changes[0]["quantity"] == float64(1)
			}
			return validation{SchemaScore: boolScore(err == nil), InstructionScore: boolScore(ok), LanguageScore: 1, ContinuityScore: boolScore(ok)}
		}
		out = append(out, freezePaired(testCase{Name: f.id, Role: "state_evaluator", Input: input, MaxTokens: 1400, Validate: validate}, f.id, 2, false, nil))
	}
	world := json.RawMessage(`{"locations":[{"locationId":"00000000-0000-4000-8000-000000000302","name":"Архив","description":"Каменный архив с дубовой дверью","visualAnchorEn":"Stone archive with an oak door"}]}`)
	for _, f := range []struct {
		id, beat string
		want     int
	}{
		{"world_absence", "Лада осмотрела прежний Архив. Мира в этом эпизоде не появлялась. О её состоянии и местонахождении ничего нового не стало известно. Здание не изменилось.", 0},
		{"world_new_npc", "Сорокалетний кузнец Борис с рыжей бородой представился Ладе. Он владеет кузницей и предложил чинить её снаряжение в будущем.", 1},
	} {
		f := f
		input := map[string]any{"newBeat": f.beat, "action": "Оглядеться и поговорить с людьми", "context": map[string]any{"world": world, "player": map[string]string{"name": "Лада"}}}
		validate := func(_ map[string]any, raw string) validation {
			normalized, err := appgen.CheckWorldProposal(f.beat, generationtarget.Target{World: world}, json.RawMessage(raw))
			var changes []map[string]any
			parseErr := json.Unmarshal(normalized, &changes)
			ok := err == nil && parseErr == nil && len(changes) == f.want
			if f.want == 1 && ok {
				ok = changes[0]["type"] == "upsert_character" && changes[0]["name"] == "Борис"
			}
			return validation{SchemaScore: boolScore(err == nil), InstructionScore: boolScore(ok), LanguageScore: 1, ContinuityScore: boolScore(ok)}
		}
		out = append(out, freezePaired(testCase{Name: f.id, Role: "world_evaluator", Input: input, MaxTokens: 1400, Validate: validate}, f.id, 2, false, nil))
	}
	// A planned goal is not a completed objective.
	for _, base := range browserRoleSuite() {
		if base.Name == "quest_evaluator" {
			raw, _ := json.Marshal(base.Input)
			var input map[string]any
			_ = json.Unmarshal(raw, &input)
			input["newBeatText"] = "Лада решила когда-нибудь поискать Мирона. Она не нашла ни нового следа, ни проходимого маршрута. Никаких действий по квестам пока не произошло."
			validate := func(obj map[string]any, _ string) validation {
				changes, ok := obj["changes"].([]any)
				good := ok && len(changes) == 0 && len(obj) == 1
				return validation{SchemaScore: boolScore(ok), InstructionScore: boolScore(good), LanguageScore: 1, ContinuityScore: boolScore(good)}
			}
			out = append(out, freezePaired(testCase{Name: "quest_planned_not_complete", Role: "quest_evaluator", Input: input, MaxTokens: 1200, Validate: validate}, "quest_planned_not_complete", 2, false, nil))
		}
	}
	return out
}
func pairedCaseByID(cases []pairedCase, id string) (pairedCase, bool) {
	for _, c := range cases {
		if c.ID == id {
			return c, true
		}
	}
	return pairedCase{}, false
}
func corpusPrevious(c pairedCase) string {
	var in struct {
		Context struct {
			Previous string `json:"previousBeatText"`
		} `json:"context"`
	}
	_ = json.Unmarshal(c.Input, &in)
	return strings.TrimSpace(in.Context.Previous)
}
