package main

import (
	ui "../trinity"
	"fmt"
)

var myUsername = "dhole"
var myUserId = "@dhole:matrix.org"

func initMsgs() {
	ui.AddMessage("!xAbiTnitnIIjlhlaWC:matrix.org", "m.text", 1234,
		"@a:matrix.org", "OLA K ASE")
	ui.AddMessage("!xAbiTnitnIIjlhlaWC:matrix.org", "m.text", 1246,
		"@b:matrix.org", "OLA K DISE")
	ui.AddMessage("!xAbiTnitnIIjlhlaWC:matrix.org", "m.text", 1249,
		"@a:matrix.org", "Pos por ahi, con la moto")
	ui.AddMessage("!xAbiTnitnIIjlhlaWC:matrix.org", "m.text", 1249,
		"@foobar:matrix.org", "Andaaa, poh no me digas      mas  hehe     toma tomate")
	ui.AddMessage("!xAbiTnitnIIjlhlaWC:matrix.org", "m.text", 1250,
		"@steve1:matrix.org", "Bon dia")
	ui.AddMessage("!xAbiTnitnIIjlhlaWC:matrix.org", "m.text", 1252,
		"@steve2:matrix.org", "Bona nit")
	ui.AddMessage("!xAbiTnitnIIjlhlaWC:matrix.org", "m.text", 1258,
		"@a:matrix.org", "Lorem ipsum dolor sit amet, consectetur adipiscing elit. Proin eget diam egestas, sollicitudin sapien eu, gravida tortor. Vestibulum eu malesuada est, vitae blandit augue. Phasellus mauris nisl, cursus quis nunc ut, vulputate condimentum felis. Aenean ut arcu orci. Morbi eget tempor diam. Curabitur semper lorem a nisi sagittis blandit. Nam non urna ligula.")
	ui.AddMessage("!xAbiTnitnIIjlhlaWC:matrix.org", "m.text", 1277,
		"@a:matrix.org", "Praesent pretium eu sapien sollicitudin blandit. Nullam lacinia est ut neque suscipit congue. In ullamcorper congue ornare. Donec lacus arcu, faucibus ut interdum eget, aliquet sed leo. Suspendisse eget congue massa, at ornare nunc. Cras ac est nunc. Morbi lacinia placerat varius. Cras imperdiet augue eu enim condimentum gravida nec nec est.")
	for i := int64(0); i < 120; i++ {
		ui.AddMessage("!xAbiTnitnIIjlhlaWC:matrix.org", "m.text", 1278+i,
			"@anon:matrix.org", fmt.Sprintf("msg #%3d", i))
	}
}

func initRooms() {
	ui.AddRoom("!xAbiTnitnIIjlhlaWC:matrix.org", "Criptica",
		"Defensant la teva privacitat des de 1984")

	ui.AddUser("!xAbiTnitnIIjlhlaWC:matrix.org", myUserId, myUsername, 100, ui.MemJoin)
	ui.AddUser("!xAbiTnitnIIjlhlaWC:matrix.org", "@a:matrix.org", "Alice", 100, ui.MemJoin)
	ui.AddUser("!xAbiTnitnIIjlhlaWC:matrix.org", "@b:matrix.org", "Bob", 100, ui.MemJoin)
	ui.AddUser("!xAbiTnitnIIjlhlaWC:matrix.org", "@e:matrix.org", "Eve", 0, ui.MemJoin)
	ui.AddUser("!xAbiTnitnIIjlhlaWC:matrix.org", "@m:matrix.org", "Mallory", 0, ui.MemJoin)
	ui.AddUser("!xAbiTnitnIIjlhlaWC:matrix.org", "@anon:matrix.org", "Anon", 0, ui.MemJoin)
	ui.AddUser("!xAbiTnitnIIjlhlaWC:matrix.org", "@steve1:matrix.org", "Steve", 0, ui.MemJoin)
	ui.AddUser("!xAbiTnitnIIjlhlaWC:matrix.org", "@steve2:matrix.org", "Steve", 0, ui.MemJoin)
	ui.AddUser("!xAbiTnitnIIjlhlaWC:matrix.org", "@foobar:matrix.org",
		"my_user_name_is_very_long", 0, ui.MemJoin)

	ui.AddRoom("!cZaiLMbuSWouYFGEDS:matrix.org", "", "")
	ui.AddUser("!cZaiLMbuSWouYFGEDS:matrix.org", myUserId, myUsername, 0, ui.MemJoin)
	ui.AddUser("!cZaiLMbuSWouYFGEDS:matrix.org", "@a:matrix.org", "Alice", 0, ui.MemJoin)

	ui.AddRoom("!aAbiTnitnIIjlhlaWC:matrix.org", "", "")
	ui.AddUser("!aAbiTnitnIIjlhlaWC:matrix.org", myUserId, myUsername, 0, ui.MemJoin)
	ui.AddUser("!aAbiTnitnIIjlhlaWC:matrix.org", "@j:matrix.org", "Johnny", 0, ui.MemJoin)

	ui.AddRoom("!bAbiTnitnIIjlhlaWC:matrix.org", "", "")
	ui.AddUser("!bAbiTnitnIIjlhlaWC:matrix.org", myUserId, myUsername, 0, ui.MemJoin)
	ui.AddUser("!bAbiTnitnIIjlhlaWC:matrix.org", "@ja:matrix.org", "Jane", 0, ui.MemJoin)

	ui.AddRoom("!cAbiTnitnIIjlhlaWC:matrix.org", "#debian-reproducible", "")
	ui.AddUser("!cAbiTnitnIIjlhlaWC:matrix.org", myUserId, myUsername, 0, ui.MemJoin)
	ui.AddUser("!cAbiTnitnIIjlhlaWC:matrix.org", "@a:matrix.org", "Alice", 0, ui.MemJoin)
	ui.AddUser("!cAbiTnitnIIjlhlaWC:matrix.org", "@b:matrix.org", "Bob", 0, ui.MemJoin)

	ui.AddRoom("!dAbiTnitnIIjlhlaWC:matrix.org", "#reproducible-builds", "")
	ui.AddUser("!dAbiTnitnIIjlhlaWC:matrix.org", myUserId, myUsername, 0, ui.MemJoin)
	ui.AddUser("!dAbiTnitnIIjlhlaWC:matrix.org", "@a:matrix.org", "Alice", 0, ui.MemJoin)
	ui.AddUser("!dAbiTnitnIIjlhlaWC:matrix.org", "@b:matrix.org", "Bob", 0, ui.MemJoin)

	ui.AddRoom("!eAbiTnitnIIjlhlaWC:matrix.org", "#openbsd", "")
	ui.AddUser("!eAbiTnitnIIjlhlaWC:matrix.org", myUserId, myUsername, 0, ui.MemJoin)
	ui.AddUser("!eAbiTnitnIIjlhlaWC:matrix.org", "@a:matrix.org", "Alice", 0, ui.MemJoin)
	ui.AddUser("!eAbiTnitnIIjlhlaWC:matrix.org", "@b:matrix.org", "Bob", 0, ui.MemJoin)

	ui.AddRoom("!fAbiTnitnIIjlhlaWC:matrix.org", "#gbdev", "")
	ui.AddUser("!fAbiTnitnIIjlhlaWC:matrix.org", myUserId, myUsername, 0, ui.MemJoin)
	ui.AddUser("!fAbiTnitnIIjlhlaWC:matrix.org", "@a:matrix.org", "Alice", 0, ui.MemJoin)
	ui.AddUser("!fAbiTnitnIIjlhlaWC:matrix.org", "@b:matrix.org", "Bob", 0, ui.MemJoin)

	ui.AddRoom("!gAbiTnitnIIjlhlaWC:matrix.org", "#archlinux", "")
	ui.AddUser("!gAbiTnitnIIjlhlaWC:matrix.org", myUserId, myUsername, 0, ui.MemJoin)
	ui.AddUser("!gAbiTnitnIIjlhlaWC:matrix.org", "@a:matrix.org", "Alice", 0, ui.MemJoin)
	ui.AddUser("!gAbiTnitnIIjlhlaWC:matrix.org", "@b:matrix.org", "Bob", 0, ui.MemJoin)

	ui.AddRoom("!hAbiTnitnIIjlhlaWC:matrix.org", "#rust", "")
	ui.AddUser("!hAbiTnitnIIjlhlaWC:matrix.org", myUserId, myUsername, 0, ui.MemJoin)
	ui.AddUser("!hAbiTnitnIIjlhlaWC:matrix.org", "@a:matrix.org", "Alice", 0, ui.MemJoin)
	ui.AddUser("!hAbiTnitnIIjlhlaWC:matrix.org", "@b:matrix.org", "Bob", 0, ui.MemJoin)

}

func main() {
	ui.Init()

	ui.SetMyUsername(myUsername)
	ui.SetMyUserId(myUserId)
	initRooms()
	initMsgs()

	ui.Start()
}
