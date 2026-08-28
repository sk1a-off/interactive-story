package generation

import (
	"errors"
	"testing"
)

func TestNarrativeNoveltyRejectsCopiedPreviousProse(t *testing.T) {
	previous := `Алекс положил ладонь на влажный мох и почувствовал ровную пульсацию под корнями древних деревьев. Нейроимплант показал уровень энергии и предложил начать постепенное подключение.`
	candidate := `Ветер стих. Алекс положил ладонь на влажный мох и почувствовал ровную пульсацию под корнями древних деревьев. Затем он посмотрел на реку.`
	if err := narrativeNoveltyError(previous, candidate); !errors.Is(err, ErrRepetitiveNarrative) {
		t.Fatalf("expected copied prose rejection, got %v", err)
	}
}

func TestNarrativeNoveltyRejectsRepeatedSentenceInsideDraft(t *testing.T) {
	previous := `Герой вышел из башни и остановился у дороги.`
	sentence := `Мария внимательно осмотрела повреждённый механизм и попросила Алекса не прикасаться к раскалённым пластинам`
	candidate := sentence + `. После короткой паузы загорелся новый индикатор. ` + sentence + `.`
	if err := narrativeNoveltyError(previous, candidate); !errors.Is(err, ErrRepetitiveNarrative) {
		t.Fatalf("expected internal repetition rejection, got %v", err)
	}
}

func TestNarrativeNoveltyAcceptsActualContinuation(t *testing.T) {
	previous := `Алекс обнаружил под корнями слабый магический ритм и отметил его частоту.`
	candidate := `Сигнал внезапно оборвался. Из воды поднялась каменная пластина с незнакомой картой, а Мария отступила и потребовала не активировать найденный знак без подготовки.`
	if err := narrativeNoveltyError(previous, candidate); err != nil {
		t.Fatalf("unexpected novelty rejection: %v", err)
	}
}

func TestNarrativeNoveltyRejectsParaphrasedParagraphInsideDraft(t *testing.T) {
	previous := `Герой вошёл в лес после полудня.`
	first := `Алекс продолжал сканировать окружение и заметил небольшие углубления в земле, светящиеся слабым голубым светом. Следы указывали в сторону старых руин. Его имплант начал анализировать направление и строить маршрут.`
	second := `Алекс снова продолжил сканировать окружение и увидел небольшие углубления в земле, которые светились слабым голубым светом. Эти следы указывали в сторону старых руин. Имплант анализировал направление и строил новый маршрут.`
	if err := narrativeNoveltyError(previous, first+"\n\n"+second); !errors.Is(err, ErrRepetitiveNarrative) {
		t.Fatalf("expected paraphrased paragraph rejection, got %v", err)
	}
}
