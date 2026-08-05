package tokens

const avgCharsPerToken = 4.0

func Estimate(text string) int {
	return len(text) / int(avgCharsPerToken)
}

func EstimateBytes(data []byte) int {
	return len(data) / int(avgCharsPerToken)
}

func BudgetOK(text string, budget int) bool {
	return Estimate(text) <= budget
}
