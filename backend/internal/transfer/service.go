package transfer

const (
	StateQueued    = "queued"
	StateSending   = "sending"
	StateReceiving = "receiving"
	StateVerified  = "verified"
	StateDone      = "done"
	StateFailed    = "failed"
	StateCanceled  = "canceled"
)

type StateMachine struct {
	FileName string
	FileSize int64
	State    string
}

func NewStateMachine() *StateMachine {
	return &StateMachine{}
}

func (s *StateMachine) Start(fileName string, fileSize int64) *StateMachine {
	s.FileName = fileName
	s.FileSize = fileSize
	s.State = StateQueued
	return s
}

func (s *StateMachine) MarkSending() {
	s.State = StateSending
}

func (s *StateMachine) MarkReceiving() {
	s.State = StateReceiving
}

func (s *StateMachine) MarkVerified() {
	s.State = StateVerified
}

func (s *StateMachine) MarkDone() {
	s.State = StateDone
}

func (s *StateMachine) MarkFailed() {
	s.State = StateFailed
}

func (s *StateMachine) MarkCanceled() {
	s.State = StateCanceled
}
