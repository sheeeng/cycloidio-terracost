package cost

import (
	"context"
	"fmt"

	"github.com/shopspring/decimal"

	"github.com/cycloidio/terracost/price"
	"github.com/cycloidio/terracost/product"
	"github.com/cycloidio/terracost/query"
)

// Backend represents a storage method used to query pricing data. It must include concrete implementations
// of all repositories.
type Backend interface {
	Product() product.Repository
	Price() price.Repository
}

// State represents a collection of all the Resource costs (either prior or planned.) It is not tied to any specific
// cloud provider or IaC tool. Instead, it is a representation of a snapshot of cloud resources at a given point
// in time, with their associated costs.
type State struct {
	Resources map[string]Resource
}

// HoursPerMonth is an approximate number of hours in a month.
// It is calculated as 365 days in a year x 24 hours in a day / 12 months in year.
const HoursPerMonth = 730

// Errors that might be returned from NewState if either a product or a price are not found.
var (
	ErrProductNotFound = fmt.Errorf("product not found")
	ErrPriceNotFound   = fmt.Errorf("price not found")
)

// NewState returns a new State from a query.Resource slice by using the Backend to fetch the pricing data.
func NewState(ctx context.Context, backend Backend, queries []query.Resource) (*State, error) {
	state := &State{Resources: make(map[string]Resource)}

	for _, res := range queries {
		// Mark the Resource as skipped if there are no valid Components.
		if len(res.Components) == 0 {
			state.Resources[res.Address] = Resource{Skipped: true}
		}

		for _, comp := range res.Components {
			prods, err := backend.Product().Filter(ctx, comp.ProductFilter)
			if err != nil {
				state.addComponent(res.Address, comp.Name, Component{Error: err})
				continue
			}
			if len(prods) < 1 {
				state.addComponent(res.Address, comp.Name, Component{Error: ErrProductNotFound})
				continue
			}
			prices, err := backend.Price().Filter(ctx, prods[0].ID, comp.PriceFilter)
			if err != nil {
				state.addComponent(res.Address, comp.Name, Component{Error: err})
				continue
			}
			if len(prices) < 1 {
				state.addComponent(res.Address, comp.Name, Component{Error: ErrPriceNotFound})
				continue
			}

			quantity := comp.MonthlyQuantity
			rate := prices[0].Value
			if quantity.Equals(decimal.Zero) {
				quantity = comp.HourlyQuantity
				rate = rate.Mul(decimal.NewFromInt(HoursPerMonth))
			}

			component := Component{
				Quantity: quantity,
				Unit:     comp.Unit,
				Rate:     rate,
				Details:  comp.Details,
			}

			state.addComponent(res.Address, comp.Name, component)
		}
	}

	return state, nil
}

// Cost returns the sum of the costs of every Resource included in this State.
func (s *State) Cost() decimal.Decimal {
	total := decimal.Zero
	for _, re := range s.Resources {
		total = total.Add(re.Cost())
	}
	return total
}

// addComponent adds the Component with given label to the Resource at given address. The Resource is created
// if it doesn't already exist.
func (s *State) addComponent(resAddress, compLabel string, component Component) {
	if _, ok := s.Resources[resAddress]; !ok {
		s.Resources[resAddress] = Resource{Components: make(map[string]Component)}
	}

	s.Resources[resAddress].Components[compLabel] = component
}
