package hookcheck

import "example.com/internal/plugin"

func use() {
	c, _ := plugin.DialBroker()  // want `plugin.DialBroker talks to the broker`
	_ = c.Call("ui.intercept")   // want `plugin.Conn.Call talks to the broker`
	_ = plugin.CleanNotice("hi") // only cleans text: fine
	go func() {
		_ = c.Notify("x") // want `plugin.Conn.Notify talks to the broker`
	}()
}
