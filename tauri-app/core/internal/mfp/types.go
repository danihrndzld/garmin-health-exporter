package mfp

// Energy is in "calories" (kcal) or "kilojoules".
type Energy struct {
	Unit  string  `json:"unit"`
	Value float64 `json:"value"`
}

func (e *Energy) Kcal() *float64 {
	if e == nil {
		return nil
	}
	v := e.Value
	if e.Unit == "kilojoules" {
		v = v / 4.184
	}
	return &v
}

// Nutrients are pointers, so a null from MFP stays empty in the CSV instead of 0.
type Nutrients struct {
	Energy       *Energy  `json:"energy"`
	Protein      *float64 `json:"protein"`
	Carbohydrate *float64 `json:"carbohydrates"`
	Fiber        *float64 `json:"fiber"`
	Sugar        *float64 `json:"sugar"`
	AddedSugars  *float64 `json:"added_sugars"`
	Fat          *float64 `json:"fat"`
	SaturatedFat *float64 `json:"saturated_fat"`
	TransFat     *float64 `json:"trans_fat"`
	Cholesterol  *float64 `json:"cholesterol"`
	Sodium       *float64 `json:"sodium"`
	Potassium    *float64 `json:"potassium"`
	Calcium      *float64 `json:"calcium"`
	Iron         *float64 `json:"iron"`
	VitaminD     *float64 `json:"vitamin_d"`
}

type DiaryItem struct {
	Type         string `json:"type"`
	Date         string `json:"date"`
	MealName     string `json:"meal_name"`
	MealPosition int    `json:"meal_position"`
	ConsumedAt   string `json:"consumed_at"`
	LoggedAt     string `json:"logged_at"`
	Food         struct {
		Description string `json:"description"`
		BrandName   string `json:"brand_name"`
		Verified    bool   `json:"verified"`
	} `json:"food"`
	ServingSize struct {
		Value      float64 `json:"value"`
		Unit       string  `json:"unit"`
		GramWeight float64 `json:"gram_weight"`
	} `json:"serving_size"`
	Servings  float64   `json:"servings"`
	Nutrients Nutrients `json:"nutritional_contents"`
	// exercise_entry fields
	Exercise struct {
		Description string `json:"description"`
		Type        string `json:"type"`
	} `json:"exercise"`
	DurationSec float64 `json:"duration"`
	Energy      *Energy `json:"energy"`
	Source      string  `json:"source"`
}

type Goals struct {
	EnergyKcal, Protein, Carbohydrate, Fat, Sodium, Sugar *float64
}
