package living

const (
	// FurusatoFormulaLabel describes the calculation basis shown in API responses and UI captions.
	FurusatoFormulaLabel = "2026年(令和8年)度 総務省簡易計算式基準"
	// FurusatoFormulaUpdatedAt records the reference formula revision date.
	FurusatoFormulaUpdatedAt = "2026-04"
	// FurusatoFormulaCaption is the full display caption for UI footers.
	FurusatoFormulaCaption = "※ 2026年(令和8年)度 総務省簡易計算式基準 (2026.04 更新)"

	FamilyStatusSingle       = "SINGLE"
	FamilyStatusMarriedNoDep = "MARRIED_NO_DEP"
	FamilyStatusMarriedDep1  = "MARRIED_DEP_1"

	OnestopMethodOnlineIAM = "ONLINE_IAM"
	OnestopMethodPostal    = "POSTAL"
	OnestopMethodTaxReturn = "TAX_RETURN"
)

// FurusatoLimitBreakdown holds intermediate values and the final estimated deduction limit in JPY.
type FurusatoLimitBreakdown struct {
	GrossIncome               int32   `json:"gross_income"`
	EffectiveSocialInsurance  int32   `json:"effective_social_insurance"`
	SocialInsuranceEstimated  bool    `json:"social_insurance_estimated"`
	EmploymentIncomeDeduction int32   `json:"employment_income_deduction"`
	TaxableIncome             int32   `json:"taxable_income"`
	ResidentTaxIncomePortion  int32   `json:"resident_tax_income_portion"`
	MarginalTaxRate           float64 `json:"marginal_tax_rate"`
	EstimatedLimit            int32   `json:"estimated_limit"`
}

// EstimateFurusatoLimit computes the simplified Furusato Nozei full-deduction limit (2,000 JPY self-burden)
// based on the Japanese Ministry of Internal Affairs and Communications (総務省) simplified formula.
func EstimateFurusatoLimit(grossIncome, socialInsurance, idecoAnnual int32, familyStatus string) FurusatoLimitBreakdown {
	if grossIncome <= 0 {
		return FurusatoLimitBreakdown{
			GrossIncome:              0,
			EffectiveSocialInsurance: 0,
			SocialInsuranceEstimated: socialInsurance <= 0,
			EstimatedLimit:           0,
		}
	}

	empDeduction := calcEmploymentIncomeDeduction(grossIncome)
	employmentIncome := grossIncome - empDeduction
	if employmentIncome < 0 {
		employmentIncome = 0
	}

	effSocial := socialInsurance
	estimatedSocial := false
	if effSocial <= 0 {
		effSocial = int32(int64(grossIncome) * 15 / 100)
		estimatedSocial = true
	}

	if idecoAnnual < 0 {
		idecoAnnual = 0
	}

	var familyDeduction int32
	switch familyStatus {
	case FamilyStatusMarriedNoDep:
		familyDeduction = 380_000
	case FamilyStatusMarriedDep1:
		familyDeduction = 760_000
	default:
		familyDeduction = 0
	}

	const residentBasicDeduction int32 = 430_000
	totalDeductions := effSocial + idecoAnnual + familyDeduction + residentBasicDeduction

	taxableIncome := employmentIncome - totalDeductions
	if taxableIncome <= 0 {
		return FurusatoLimitBreakdown{
			GrossIncome:               grossIncome,
			EffectiveSocialInsurance:  effSocial,
			SocialInsuranceEstimated:  estimatedSocial,
			EmploymentIncomeDeduction: empDeduction,
			TaxableIncome:             0,
			ResidentTaxIncomePortion:  0,
			MarginalTaxRate:           0,
			EstimatedLimit:            0,
		}
	}

	// Resident tax income-proportional amount (住民税所得割額 = 課税標準 × 10%)
	residentTaxIncomePortion := taxableIncome / 10
	if residentTaxIncomePortion <= 0 {
		return FurusatoLimitBreakdown{
			GrossIncome:               grossIncome,
			EffectiveSocialInsurance:  effSocial,
			SocialInsuranceEstimated:  estimatedSocial,
			EmploymentIncomeDeduction: empDeduction,
			TaxableIncome:             taxableIncome,
			ResidentTaxIncomePortion:  0,
			MarginalTaxRate:           0,
			EstimatedLimit:            0,
		}
	}

	baseTaxRate := marginalIncomeTaxRate(taxableIncome)
	denominator := 1.0 - 0.10 - (baseTaxRate * 1.021)
	if denominator <= 0.05 {
		denominator = 0.05
	}

	rawLimit := (float64(residentTaxIncomePortion)*0.20)/denominator + 2000.0
	// Floor to 100 JPY units for conservative, clean estimation
	roundedLimit := (int32(rawLimit) / 100) * 100
	if roundedLimit < 2000 {
		roundedLimit = 0
	}

	return FurusatoLimitBreakdown{
		GrossIncome:               grossIncome,
		EffectiveSocialInsurance:  effSocial,
		SocialInsuranceEstimated:  estimatedSocial,
		EmploymentIncomeDeduction: empDeduction,
		TaxableIncome:             taxableIncome,
		ResidentTaxIncomePortion:  residentTaxIncomePortion,
		MarginalTaxRate:           baseTaxRate,
		EstimatedLimit:            roundedLimit,
	}
}

// calcEmploymentIncomeDeduction calculates the Japanese salary income deduction (給与所得控除).
func calcEmploymentIncomeDeduction(gross int32) int32 {
	g := int64(gross)
	switch {
	case g <= 1_625_000:
		return 550_000
	case g <= 1_800_000:
		d := (g*40)/100 - 100_000
		if d < 550_000 {
			return 550_000
		}
		return int32(d)
	case g <= 3_600_000:
		return int32((g*30)/100 + 80_000)
	case g <= 6_600_000:
		return int32((g*20)/100 + 440_000)
	case g <= 8_500_000:
		return int32((g*10)/100 + 1_100_000)
	default:
		return 1_950_000
	}
}

// marginalIncomeTaxRate returns the Japanese progressive national income tax bracket rate.
func marginalIncomeTaxRate(taxable int32) float64 {
	switch {
	case taxable <= 1_950_000:
		return 0.05
	case taxable <= 3_300_000:
		return 0.10
	case taxable <= 6_950_000:
		return 0.20
	case taxable <= 9_000_000:
		return 0.23
	case taxable <= 18_000_000:
		return 0.33
	case taxable <= 40_000_000:
		return 0.40
	default:
		return 0.45
	}
}

