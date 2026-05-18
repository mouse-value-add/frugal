package search

import (
	"context"
	"testing"
)

type fakeSearcher struct {
	name string
	cost float64
}

func (f *fakeSearcher) Name() string         { return f.name }
func (f *fakeSearcher) CostPerCall() float64 { return f.cost }
func (f *fakeSearcher) Search(_ context.Context, _ Query) (Results, error) {
	return Results{}, nil
}

func TestRouteCheapest_PicksLowestCost(t *testing.T) {
	a := &fakeSearcher{name: "a", cost: 0.01}
	b := &fakeSearcher{name: "b", cost: 0.003}
	c := &fakeSearcher{name: "c", cost: 0.005}
	got := RouteCheapest([]Searcher{a, b, c})
	if got.Name() != "b" {
		t.Errorf("RouteCheapest: got %q, want b (cheapest)", got.Name())
	}
}

func TestRouteCheapest_TieKeepsInputOrder(t *testing.T) {
	a := &fakeSearcher{name: "a", cost: 0.003}
	b := &fakeSearcher{name: "b", cost: 0.003}
	got := RouteCheapest([]Searcher{a, b})
	if got.Name() != "a" {
		t.Errorf("RouteCheapest tie: got %q, want a (first in input)", got.Name())
	}
}

func TestRouteCheapest_EmptyReturnsNil(t *testing.T) {
	if got := RouteCheapest(nil); got != nil {
		t.Errorf("RouteCheapest(nil) = %v, want nil", got)
	}
}

func TestFind_NameMatch(t *testing.T) {
	a := &fakeSearcher{name: "a", cost: 0.01}
	b := &fakeSearcher{name: "b", cost: 0.003}
	if got := Find([]Searcher{a, b}, "b"); got.Name() != "b" {
		t.Errorf("Find(b): got %v", got)
	}
	if got := Find([]Searcher{a, b}, "missing"); got != nil {
		t.Errorf("Find(missing): got %v, want nil", got)
	}
}
