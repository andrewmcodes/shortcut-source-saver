package tui

type selection struct {
	ids map[string]bool
}

func newSelection() *selection {
	return &selection{ids: map[string]bool{}}
}

func (s *selection) has(id string) bool {
	return s.ids[id]
}

func (s *selection) add(id string) {
	s.ids[id] = true
}

func (s *selection) remove(id string) {
	delete(s.ids, id)
}

func (s *selection) toggle(id string) bool {
	if s.ids[id] {
		delete(s.ids, id)
		return false
	}
	s.ids[id] = true
	return true
}

func (s *selection) clear() {
	for id := range s.ids {
		delete(s.ids, id)
	}
}

func (s *selection) count() int {
	return len(s.ids)
}
