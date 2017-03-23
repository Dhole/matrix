package main

import "fmt"
import "github.com/matrix-org/gomatrix"

func main() {
	fmt.Println("Start!")
	user := "ray_test"
	pass := "CiIYIrD3OtSuudJB"
	cli, _ := gomatrix.NewClient("https://matrix.org", "", "")

	resp, err := cli.Login(&gomatrix.ReqLogin{
		Type:     "m.login.password",
		User:     user,
		Password: pass,
	})
	if err != nil {
		panic(err)
	}

	fmt.Println("Token:", resp.AccessToken)
	cli.SetCredentials(resp.UserID, resp.AccessToken)

	resp_sync, err := cli.SyncRequest(30000, "", "", false, "offline")
	if err != nil {
		panic(err)
	}
	fmt.Println("Joined rooms...")
	for room_id, _ := range resp_sync.Rooms.Join {
		fmt.Println(room_id)
		resp_joined_mem, err := cli.JoinedMembers(room_id)
		if err != nil {
			panic(err)
		}
		for user_id, user_data := range resp_joined_mem.Joined {
			disp_name := ""
			if user_data.DisplayName != nil {
				disp_name = *user_data.DisplayName
			}
			fmt.Println("\t", user_id, ":", disp_name)
		}
	}

	cli.SendText("!cZaiLMbuSWouYFGEDS:matrix.org", "OLA K ASE")

	syncer := cli.Syncer.(*gomatrix.DefaultSyncer)
	syncer.OnEventType("m.room.message", func(ev *gomatrix.Event) {
		fmt.Println("Message: ", ev)
	})

	// Blocking version
	if err := cli.Sync(); err != nil {
		fmt.Println("Sync() returned ", err)
	}
}
