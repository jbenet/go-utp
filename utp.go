package utp

const (
	VERSION = 1

	ST_DATA  = 0
	ST_FIN   = 1
	ST_STATE = 2
	ST_RESET = 3
	ST_SYN   = 4

	EXT_NONE          = 0
	EXT_SELECTIVE_ACK = 1

	HEADER_SIZE = 20
	MTU         = 1400
	MSS         = MTU - HEADER_SIZE
	WINDOW_SIZE = 180

	CS_SYN_CLOSE = iota
	CS_SYN_LISTEN
	CS_SYN_SENT
	CS_SYN_CONNECTED
	CS_SYN_CLOSING
)

type timeoutError struct{}

func (e *timeoutError) Error() string   { return "i/o timeout" }
func (e *timeoutError) Timeout() bool   { return true }
func (e *timeoutError) Temporary() bool { return true }
