// Holdout evaluation: train on the oldest 80 % of the samples, test on the newest 20 % (like real use: the model
// predicts the future from the past).
package ml

const holdoutShare = 0.2

// Labeled is a sample with an optional "what the rules said" class, to compare the model with the rules.
type Labeled struct {
	Sample
	RulesClass string // "" when no rule matched
	ByUser     bool   // category set by hand: the ground truth that matters most
}

// Evaluation compares the model and the rules on the holdout.
type Evaluation struct {
	Tested        int
	Accuracy      float64 // model, on every test sample
	UserTested    int
	ModelOnUser   float64 // model, on test samples the user categorized by hand
	RulesOnUser   float64 // rules, on the same samples
	TrainedOnRest *Model
}

// Evaluate trains on the first 80 % of samples (sorted oldest first) and tests on the rest.
func Evaluate(samples []Labeled) Evaluation {
	split := len(samples) - int(float64(len(samples))*holdoutShare)
	train := make([]Sample, split)
	for i, s := range samples[:split] {
		train[i] = s.Sample
	}
	model := Train(train)
	e := Evaluation{TrainedOnRest: model}
	var right, userModelRight, userRulesRight int
	for _, s := range samples[split:] {
		predicted, _ := model.Predict(s.Features)
		e.Tested++
		if predicted == s.Class {
			right++
		}
		if s.ByUser {
			e.UserTested++
			if predicted == s.Class {
				userModelRight++
			}
			if s.RulesClass == s.Class {
				userRulesRight++
			}
		}
	}
	e.Accuracy = ratio(right, e.Tested)
	e.ModelOnUser = ratio(userModelRight, e.UserTested)
	e.RulesOnUser = ratio(userRulesRight, e.UserTested)
	return e
}

func ratio(a, b int) float64 {
	if b == 0 {
		return 0
	}
	return float64(a) / float64(b)
}
