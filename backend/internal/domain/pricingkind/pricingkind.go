package pricingkind

// Kind identifies the mutually-exclusive pricing selector owned by a revision.
type Kind string

const (
	Standard   Kind = "standard"
	Tiered     Kind = "tiered"
	PeakValley Kind = "peak_valley"
)

const (
	RoleStandard  = "standard"
	RoleTierBase  = "tier_base"
	RoleTierAbove = "tier_above"
	RolePeak      = "peak"
	RoleOffpeak   = "offpeak"
)

const (
	SelectionNotEvaluated  = "not_evaluated"
	SelectionNotApplicable = "not_applicable"
	SelectionSelected      = "selected"
	SelectionUnresolved    = "unresolved"
)

func (k Kind) Valid() bool {
	return k == Standard || k == Tiered || k == PeakValley
}

func Kinds() []Kind {
	return []Kind{Standard, Tiered, PeakValley}
}

func RolesFor(k Kind) []string {
	switch k {
	case Standard:
		return []string{RoleStandard}
	case Tiered:
		return []string{RoleTierBase, RoleTierAbove}
	case PeakValley:
		return []string{RolePeak, RoleOffpeak}
	default:
		return nil
	}
}

func RoleValid(role string) bool {
	return role == RoleStandard || role == RoleTierBase || role == RoleTierAbove || role == RolePeak || role == RoleOffpeak
}
