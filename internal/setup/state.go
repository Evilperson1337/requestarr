package setup

type Step string

const (
	StepWelcome        Step = "welcome"
	StepGeneral        Step = "general"
	StepAuthentication Step = "authentication"
	StepProviders      Step = "providers"
	StepLibraries      Step = "libraries"
	StepNotifications  Step = "notifications"
	StepPolicy         Step = "policy"
	StepReview         Step = "review"
	StepFinish         Step = "finish"
)

type State struct {
	Complete bool
	Current  Step
}
