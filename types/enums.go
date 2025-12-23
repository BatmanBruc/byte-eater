package types

type ChatState string

const (
	StateStart       ChatState = "start"
	StateChooseExt   ChatState = "choose_ext"
	StateWaitingFile ChatState = "waiting_file"
	StateProcessing  ChatState = "processing"
	StateReady       ChatState = "ready"
	StateError       ChatState = "error"
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
