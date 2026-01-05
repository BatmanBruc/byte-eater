package types

type ChatState string

const (
	StateChooseExt  ChatState = "choose_ext"
	StateProcessing ChatState = "processing"
	StateReady      ChatState = "ready"
	StateError      ChatState = "error"
)

const (
	ClientErrorDefault         string = "Произошла ошибка сервиса. Попробуйте снова."
	ClientErrorNotFoundCommand string = "Команда не найдена"
)

const (
	MessageTypeImage string = "image"
)

const (
	CommandMessageStart string = "Привет! Это сервис по конвертации файлов. Скидывай файл и выбирай формат."
)
