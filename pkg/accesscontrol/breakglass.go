package accesscontrol

import "context"

type breakGlass struct{ next DecisionPoint }

type resourceEnumerator interface {
	AllIDsOfType(context.Context, ResourceKind) ([]int64, error)
}

// WithBreakGlass keeps emergency root access explicit and outside policy data.
// Ordinary users, including system auditors, always pass through RBAC v4.
func WithBreakGlass(next DecisionPoint) DecisionPoint { return breakGlass{next: next} }

func (b breakGlass) Can(ctx context.Context, principal Principal, verb Verb, resource Resource) (bool, error) {
	if principal.BreakGlassRoot {
		return true, nil
	}
	return b.next.Can(ctx, principal, verb, resource)
}

func (b breakGlass) CanCapability(ctx context.Context, principal Principal, capability string, resource Resource) (bool, error) {
	if principal.BreakGlassRoot {
		return true, nil
	}
	return b.next.CanCapability(ctx, principal, capability, resource)
}

func (b breakGlass) CanGlobal(ctx context.Context, principal Principal, capability string) (bool, error) {
	if principal.BreakGlassRoot {
		return true, nil
	}
	return b.next.CanGlobal(ctx, principal, capability)
}

func (b breakGlass) VisibleIDs(ctx context.Context, principal Principal, verb Verb, kind ResourceKind) ([]int64, error) {
	if principal.BreakGlassRoot {
		if enumerator, ok := b.next.(resourceEnumerator); ok {
			return enumerator.AllIDsOfType(ctx, kind)
		}
		return nil, nil
	}
	return b.next.VisibleIDs(ctx, principal, verb, kind)
}
