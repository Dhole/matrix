package main

import (
	ui "../trinity"
	"fmt"
	"github.com/matrix-org/gomatrix"
	//"github.com/pkg/profile"
	"github.com/spf13/viper"
	"strings"
)

type config struct {
	Username    string
	UserID      string
	DisplayName string
	Password    string
	Homeserver  string
}

type GenMap map[string]interface{}

func NewGenMap() GenMap {
	return GenMap(make(map[string]interface{}))
}

func (m *GenMap) StringKey(k string) string {
	vg, ok := (*m)[k]
	if !ok {
		return ""
	}
	v, ok := vg.(string)
	if !ok {
		return ""
	}
	return v
}

func AddRoomData(cli *gomatrix.Client, roomID string) {
	res := NewGenMap()
	cli.StateEvent(roomID, "m.room.name", "", &res)
	name := res.StringKey("name")
	cli.StateEvent(roomID, "m.room.topic", "", &res)
	topic := res.StringKey("topic")
	cli.StateEvent(roomID, "m.room.canonical_alias", "", &res)
	canonicalAlias := res.StringKey("alias")
	ui.AddConsoleMessage(fmt.Sprintf("Adding room (%s) \"%s\" | \"%s\": %s",
		roomID, name, canonicalAlias, topic))
	ui.AddRoom(roomID, name, canonicalAlias, topic)
	resJoinedMem, err := cli.JoinedMembers(roomID)
	if err != nil {
		panic(err)
	}
	//fmt.Println(roomID, "Batch add")
	for userID, userData := range resJoinedMem.Joined {
		username := ""
		if userData.DisplayName != nil {
			username = *userData.DisplayName
		}
		ui.AddUserBatch(roomID, userID, username, 0, ui.MemJoin)
	}
	//fmt.Println(roomID, "Batch add complete")
	ui.AddUserBatchFinish()
	//fmt.Println(roomID, "Batch finish complete")
}

func main() {
	//defer profile.Start().Stop()
	viper.SetConfigType("toml")
	viper.SetConfigName("morpheus")
	viper.AddConfigPath(".")

	if err := viper.ReadInConfig(); err != nil {
		panic(fmt.Errorf("Fatal error config file: %s \n", err))
	}

	mustExistKeys := []string{"Username", "Password", "Homeserver"}
	for _, key := range mustExistKeys {
		if !viper.IsSet(key) {
			panic(fmt.Errorf("Key %s not found in config file", key))
		}
	}
	var c config
	if err := viper.Unmarshal(&c); err != nil {
		panic(fmt.Errorf("Fatal error decoding config file, %v", err))
	}

	c.UserID = fmt.Sprintf("@%s:%s", c.Username, strings.TrimPrefix(c.Homeserver, "https://"))

	ui.Init()
	ui.SetMyDisplayName(c.DisplayName)
	ui.SetMyUserID(c.UserID)

	cli, _ := gomatrix.NewClient(c.Homeserver, "", "")
	// Set event callbacks
	ui.SetCallSendText(func(roomID, body string) {
		_, err := cli.SendText(roomID, body)
		if err != nil {
			ui.AddConsoleMessage(fmt.Sprint("send:", err))
			return
		}
	})
	ui.SetCallJoinRoom(func(roomIDorAlias string) {
		resJoin, err := cli.JoinRoom(roomIDorAlias, "", nil)
		if err != nil {
			ui.AddConsoleMessage(fmt.Sprint("join:", err))
			return
		}
		roomID := resJoin.RoomID
		AddRoomData(cli, roomID)
	})
	ui.SetCallLeaveRoom(func(roomID string) {
		_, err := cli.LeaveRoom(roomID)
		if err != nil {
			ui.AddConsoleMessage(fmt.Sprint("leave:", err))
			return
		}
		roomName, err := ui.DelRoom(roomID)
		if err != nil {
			ui.AddConsoleMessage(fmt.Sprint("leave:", err))
		} else {
			ui.AddConsoleMessage(fmt.Sprintf("Left room (%s) \"%s\"",
				roomID, roomName))
		}
	})

	exit := make(chan bool)
	go func() {
		if err := ui.Start(); err != nil {
			panic(err)
		}
		exit <- true
	}()

	ui.AddConsoleMessage(fmt.Sprintf("Logging in to %s ...", c.Homeserver))
	res, err := cli.Login(&gomatrix.ReqLogin{
		Type:     "m.login.password",
		User:     c.Username,
		Password: c.Password,
	})
	if err != nil {
		panic(err)
	}

	//fmt.Println("Token:", res.AccessToken)
	ui.AddConsoleMessage(fmt.Sprintf("Logged in to %s", c.Homeserver))
	ui.Debugf("AccessToken:\n%s", res.AccessToken)
	cli.SetCredentials(res.UserID, res.AccessToken)

	ui.AddConsoleMessage("Doing initial sync request ...")
	resSync, err := cli.SyncRequest(30000, "", "", false, "online")
	if err != nil {
		panic(err)
	}
	ui.AddConsoleMessage("Initial sync request finished")
	//fmt.Println("Joined rooms...")
	for roomID, roomHist := range resSync.Rooms.Join {
		//fmt.Println(roomID)
		ui.Debugf("room %s has %d timeline.events",
			roomID, len(roomHist.Timeline.Events))
		for _, ev := range roomHist.Timeline.Events {
			body, ok := ev.Body()
			if ok {
				ui.AddMessage(roomID, ev.Type,
					int64(ev.Timestamp/1000), ev.Sender, body)
			}
			//else {
			//	ui.AddConsoleMessage(fmt.Sprintf("%+v", ev))
			//}
		}
		AddRoomData(cli, roomID)
	}

	// TODO: Do this only if the already set display name doesn't match the config
	//if c.DisplayName != "" {
	//	cli.SetDisplayName(c.DisplayName)
	//}

	syncer := cli.Syncer.(*gomatrix.DefaultSyncer)
	syncer.OnEventType("m.room.message", func(ev *gomatrix.Event) {
		//fmt.Println("Message: ", ev)
		msgType, ok := ev.Body()
		if !ok {
			msgType = "?"
		}
		body, ok := ev.Body()
		if !ok {
			body = "???"
		}
		err := ui.AddMessage(ev.RoomID, msgType, int64(ev.Timestamp/1000), ev.Sender, body)
		if err != nil {
			fmt.Println(err)
		}
	})

	// Blocking version
	go func() {
		if err := cli.Sync(); err != nil {
			fmt.Println("Sync() returned ", err)
		}
	}()
	<-exit
}
