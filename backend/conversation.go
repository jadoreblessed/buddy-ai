package main

import (
	"fmt"
	"regexp"
	"strings"
	"unicode"
)

var codeRequestPattern = regexp.MustCompile(`(?i)(напиши|напишите|создай|создайте|сгенерируй|сгенерируйте|реализуй|реализуйте|сделай|сделайте|реши|решите|переведи|переведите).{0,60}(код|калькулятор|программ|скрипт|функци|бот|сайт|приложени|алгоритм|уравнени|python|питон|javascript|typescript|java|kotlin|swift|rust|golang|sql|c\+\+|html|css)`)

var greetingWords = map[string]bool{
	"привет": true, "приветик": true, "здравствуй": true, "здравствуйте": true,
	"хай": true, "hello": true, "hi": true, "приветствую": true, "салют": true,
	"йо": true, "добрый": true, "доброе": true, "доброго": true,
}

func normalizeConversation(value string) string {
	var builder strings.Builder
	for _, r := range strings.ToLower(value) {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			builder.WriteRune(r)
		} else {
			builder.WriteByte(' ')
		}
	}
	return strings.Join(strings.Fields(builder.String()), " ")
}

func hasPhrase(text string, phrases ...string) bool {
	for _, phrase := range phrases {
		if strings.Contains(text, phrase) {
			return true
		}
	}
	return false
}

func isShortMessage(text string, limit int) bool {
	return len(strings.Fields(text)) <= limit
}

func isHowAreYou(text string) bool {
	return isShortMessage(text, 6) && hasPhrase(text,
		"как дела", "как ты", "как настроение", "как жизнь", "как сам", "как вы", "что нового")
}

func isWhoAreYou(text string) bool {
	return isShortMessage(text, 6) && hasPhrase(text,
		"кто ты", "ты кто", "как тебя зовут", "твое имя", "твоё имя", "ты бот", "ты человек", "расскажи о себе")
}

func isCapabilities(text string) bool {
	return isShortMessage(text, 7) && hasPhrase(text,
		"что ты умеешь", "что умеешь", "что ты можешь", "что можешь",
		"чем ты можешь помочь", "чем можешь помочь", "твои возможности",
		"как ты можешь помочь", "помощь")
}

func isThanks(text string) bool {
	return isShortMessage(text, 4) && hasPhrase(text, "спасибо", "благодарю", "спс", "thanks", "thank you")
}

func isGoodbye(text string) bool {
	switch text {
	case "пока", "до свидания", "до встречи", "увидимся", "всего доброго", "хорошего дня":
		return true
	}
	return false
}

func isGreeting(text string) bool {
	fields := strings.Fields(text)
	if len(fields) == 0 || len(fields) > 5 {
		return false
	}
	return greetingWords[fields[0]] || text == "добрый день" || text == "добрый вечер" ||
		text == "доброе утро" || text == "доброго дня"
}

func firstName(fullName string) string {
	fields := strings.Fields(fullName)
	if len(fields) == 0 {
		return ""
	}
	return fields[0]
}

func starterSuggestions() []string {
	return []string{
		"Что дальше по плану?",
		"Какие доступы нужны?",
		"Как оформить отгул?",
		"Кто мой наставник?",
	}
}

func greetingAnswer(plan Plan) BuddyAnswer {
	hello := "Привет"
	if name := firstName(plan.Intern); name != "" {
		hello += ", " + name
	}
	return BuddyAnswer{
		Answer:      fmt.Sprintf("%s! Я рядом — твой AI-наставник по адаптации. Помогу с планом, доступами, материалами, отгулом и связью с наставником. С чего начнём?", hello),
		Sources:     []AnswerSource{},
		Suggestions: starterSuggestions(),
	}
}

func howAreYouAnswer() BuddyAnswer {
	return BuddyAnswer{
		Answer:      "У меня всё отлично — я на связи и готов помочь. Могу подсказать следующий шаг по плану, объяснить, как запросить доступ, или показать материалы.",
		Sources:     []AnswerSource{},
		Suggestions: starterSuggestions(),
	}
}

func whoAreYouAnswer() BuddyAnswer {
	return BuddyAnswer{
		Answer:      "Я рядом — AI-buddy для стажёров. Отвечаю по назначенному плану и тестовым материалам, а действия с заявками выполняю только после твоего подтверждения.",
		Sources:     []AnswerSource{},
		Suggestions: starterSuggestions(),
	}
}

func capabilitiesAnswer() BuddyAnswer {
	return BuddyAnswer{
		Answer:      "Помогаю с адаптацией: объясняю шаги плана, подсказываю доступы, показываю материалы, помогаю с отгулом и сообщением наставнику. Вопросы вне этой темы не разбираю.",
		Sources:     []AnswerSource{},
		Suggestions: starterSuggestions(),
	}
}

func thanksAnswer() BuddyAnswer {
	return BuddyAnswer{
		Answer:      "Пожалуйста! Если нужно, продолжим с планом или разберём следующий шаг.",
		Sources:     []AnswerSource{},
		Suggestions: starterSuggestions(),
	}
}

func goodbyeAnswer() BuddyAnswer {
	return BuddyAnswer{
		Answer:  "До встречи! План и материалы остаются в разделах слева.",
		Sources: []AnswerSource{},
	}
}

func scopeAnswer() BuddyAnswer {
	return BuddyAnswer{
		Answer:      "Это вне моей специализации. Я помогаю только с адаптацией стажёра: планом, доступами, материалами, отгулом и связью с наставником. Могу подсказать следующий шаг или объяснить, как запросить доступ.",
		Sources:     []AnswerSource{},
		Suggestions: starterSuggestions(),
	}
}

func unknownAnswer() BuddyAnswer {
	return BuddyAnswer{
		Answer:      "В тестовых материалах нет ответа на этот вопрос. Я помогаю только с адаптацией стажёра: планом, доступами, материалами, отгулом и связью с наставником. Уточни детали у наставника.",
		Sources:     []AnswerSource{},
		Suggestions: starterSuggestions(),
	}
}

func asksForCodeGeneration(question string) bool {
	return codeRequestPattern.MatchString(question)
}

func isFollowUp(text string) bool {
	return isShortMessage(text, 8) && hasPhrase(text,
		"подробнее", "подробней", "почему", "а как", "как это", "что дальше",
		"что еще", "что ещё", "расскажи", "объясни", "уточни", "в смысле", "и что")
}

func conversationReply(plan Plan, question string) (BuddyAnswer, bool) {
	text := normalizeConversation(question)
	if text == "" {
		return BuddyAnswer{}, false
	}
	switch {
	case isHowAreYou(text):
		return howAreYouAnswer(), true
	case isWhoAreYou(text):
		return whoAreYouAnswer(), true
	case isCapabilities(text):
		return capabilitiesAnswer(), true
	case isThanks(text):
		return thanksAnswer(), true
	case isGoodbye(text):
		return goodbyeAnswer(), true
	case isGreeting(text):
		return greetingAnswer(plan), true
	}
	return BuddyAnswer{}, false
}
