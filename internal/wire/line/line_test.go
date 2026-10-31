package line

import (
	"bufio"
	"fmt"
	"github.com/lmctl-ai/lmmock/internal/wire"
	"github.com/lmctl-ai/lmmock/internal/wire/testutil"
	"net"
	"testing"
)

func TestPolicyIsOnlyLmcode(t *testing.T) {
	body := `@commands fn commands(s: Session) -> void implements LineHandler { while (s.recv()) { if (s.body()=="PING") { s.send("PONG"); } else { s.send("ERR "+s.body()); } } } bind("connection","commands");`
	e, err := wire.New("line", Layer{}, testutil.Compile(t, "line", body), nil)
	if err != nil {
		t.Fatal(err)
	}
	c, err := net.Dial("tcp", testutil.Start(t, e))
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	reader := bufio.NewReader(c)
	for _, pair := range [][2]string{{"PING", "PONG\n"}, {"other", "ERR other\n"}} {
		if _, err = fmt.Fprintln(c, pair[0]); err != nil {
			t.Fatal(err)
		}
		got, err := reader.ReadString('\n')
		if err != nil || got != pair[1] {
			t.Fatal(got, err)
		}
	}
}
