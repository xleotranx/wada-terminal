package core

import (
	"fmt"
	"sort"
	"strings"
	"time"
)

// ask records a child's question, types it into its parent's input, and
// returns the question. The asker waits by reading the question back.
func (s *Server) ask(child, parent int, text string) (Question, error) {
	text = strings.TrimSpace(text)
	if text == "" {
		return Question{}, fmt.Errorf("ask needs a question")
	}
	childRec := s.terminal(child)
	if child == 0 || childRec == nil {
		return Question{}, fmt.Errorf("ask needs the terminal asking (WADA_TERMINAL_ID)")
	}
	if parent == 0 {
		parent = childRec.start.Parent
	}
	if parent == 0 {
		return Question{}, fmt.Errorf("this terminal has no parent to ask")
	}
	parentRec := s.terminal(parent)
	if parentRec == nil {
		return Question{}, fmt.Errorf("no parent terminal %d", parent)
	}
	if !parentRec.record().Alive {
		return Question{}, fmt.Errorf("the parent terminal %d is not running", parent)
	}
	s.questionMu.Lock()
	s.nextQuestion++
	question := Question{
		ID:     fmt.Sprintf("q%d", s.nextQuestion),
		Child:  child,
		Parent: parent,
		Text:   text,
		Asked:  time.Now().UnixMilli(),
	}
	s.questions[question.ID] = &question
	s.wakeQuestionsLocked()
	s.questionMu.Unlock()
	s.notifyQuestion(&question, childRec)
	return question, nil
}

// notifyQuestion types the question and the command that answers it into the
// parent's input. The parent is a person or an agent reading its terminal.
func (s *Server) notifyQuestion(q *Question, child *terminal) {
	parent := s.terminal(q.Parent)
	if parent == nil {
		return
	}
	from := address(child.id)
	if child.start.Name != "" {
		from += " " + child.start.Name
	}
	command := fmt.Sprintf("wada-terminal core answer %s --question %s \"...\"", address(q.Child), q.ID)
	note := fmt.Sprintf("\n<wada from=\"%s\" question=\"%s\">%s</wada>\nanswer: %s\n",
		escapeNote(from), q.ID, escapeNote(q.Text), command)
	_ = parent.send([]byte(note + "\r"))
}

// questionList returns every question, oldest first.
func (s *Server) questionList() []Question {
	s.questionMu.Lock()
	questions := make([]Question, 0, len(s.questions))
	for _, q := range s.questions {
		questions = append(questions, *q)
	}
	s.questionMu.Unlock()
	sort.Slice(questions, func(i, j int) bool {
		if questions[i].Asked == questions[j].Asked {
			return questions[i].ID < questions[j].ID
		}
		return questions[i].Asked < questions[j].Asked
	})
	return questions
}

// questionByID returns one question.
func (s *Server) questionByID(id string) (Question, error) {
	s.questionMu.Lock()
	defer s.questionMu.Unlock()
	q := s.questions[id]
	if q == nil {
		return Question{}, fmt.Errorf("no question %q", id)
	}
	return *q, nil
}

// answerQuestion sets an open question's answer.
func (s *Server) answerQuestion(id, answer string) (Question, error) {
	answer = strings.TrimSpace(answer)
	if answer == "" {
		return Question{}, fmt.Errorf("answer needs text")
	}
	s.questionMu.Lock()
	defer s.questionMu.Unlock()
	q := s.questions[id]
	if q == nil {
		return Question{}, fmt.Errorf("no question %q", id)
	}
	if q.Closed || q.Answer != "" {
		return Question{}, fmt.Errorf("question %s is already answered or closed", id)
	}
	q.Answer = answer
	q.Answered = time.Now().UnixMilli()
	s.wakeQuestionsLocked()
	return *q, nil
}

// cancelQuestion closes a question whose asker stopped waiting.
func (s *Server) cancelQuestion(id string) error {
	s.questionMu.Lock()
	defer s.questionMu.Unlock()
	q := s.questions[id]
	if q == nil {
		return fmt.Errorf("no question %q", id)
	}
	q.Closed = true
	s.wakeQuestionsLocked()
	return nil
}

// closeQuestionsFor closes a terminal's open questions when it ends.
func (s *Server) closeQuestionsFor(terminal int) {
	s.questionMu.Lock()
	defer s.questionMu.Unlock()
	changed := false
	for _, q := range s.questions {
		if q.Child == terminal || q.Parent == terminal {
			q.Closed = true
			changed = true
		}
	}
	if changed {
		s.wakeQuestionsLocked()
	}
}

// wakeQuestionsLocked releases waiters after a question changes. It is called
// with questionMu held.
func (s *Server) wakeQuestionsLocked() {
	close(s.questionWake)
	s.questionWake = make(chan struct{})
}

// waitQuestion waits for a question to be answered or closed, returning the
// current value at the timeout.
func (s *Server) waitQuestion(id string, timeout time.Duration) (Question, error) {
	deadline := time.Now().Add(timeout)
	for {
		s.questionMu.Lock()
		q := s.questions[id]
		if q == nil {
			s.questionMu.Unlock()
			return Question{}, fmt.Errorf("no question %q", id)
		}
		current := *q
		wake := s.questionWake
		s.questionMu.Unlock()
		if current.Answer != "" || current.Closed || timeout <= 0 {
			return current, nil
		}
		remaining := time.Until(deadline)
		if remaining <= 0 {
			return current, nil
		}
		timer := time.NewTimer(remaining)
		select {
		case <-wake:
			if !timer.Stop() {
				<-timer.C
			}
		case <-timer.C:
			return current, nil
		}
	}
}
