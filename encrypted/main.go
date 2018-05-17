package main

import (
	mo "./gomatrixolm"
	"fmt"
	mat "github.com/Dhole/gomatrix"
	"github.com/op/go-logging"
	"os"
	"time"
)

func main() {
	password := os.Args[1]
	userID := "@ray_test:matrix.org"
	username := "ray_test"
	homeserver := "https://matrix.org"
	deviceID := "5un3HpnWE04"
	deviceDisplayName := "go-olm-dev02"

	db, err := mo.OpenCryptoDB("test.db")
	if err != nil {
		panic(err)
	}

	logFormat := logging.MustStringFormatter(
		`%{time:2006-01-02 15:04:05} %{color}%{level} %{shortfile} %{callpath:4} ` +
			`%{color:reset}%{message}`)
	logBackend := logging.NewLogBackend(os.Stderr, "", 0)
	logging.SetBackend(logBackend)
	logging.SetFormatter(logFormat)
	logging.SetLevel(logging.DEBUG, "")

	log := logging.MustGetLogger("client")

	cli, _ := mo.NewClient(homeserver, userID, deviceID, password, db, log)
	fmt.Println("Logging in...")
	resLogin, err := cli.Login(&mat.ReqLogin{
		Type:                     "m.login.password",
		User:                     username,
		Password:                 password,
		DeviceID:                 deviceID,
		InitialDeviceDisplayName: deviceDisplayName,
	})
	if err != nil {
		panic(err)
	}
	cli.SetCredentials(resLogin.UserID, resLogin.AccessToken)

	res := &mo.RespSync{}
	fmt.Println("Initial sync...")
	// First sync
	for {
		res, err = cli.SyncRequest(30000, res.NextBatch, "", false, "online")
		if err != nil {
			time.Sleep(10)
			continue
		}
		//Filter(res, roomID)
		break
	}
	fmt.Printf("%+v\n", res)

	//cli.Crypto.ForEachRoom(func(_ mat.RoomID, room *mo.Room) error {
	//	fmt.Printf("%s - %s\n", room.ID(), room.EncryptionAlg())
	//	room.ForEachUser(mo.MemJoin, func(_ mat.UserID, user *mo.User) error {
	//		fmt.Printf("\t%s\n", user.ID())
	//		return nil
	//	})
	//	fmt.Println()
	//	return nil
	//})

	//// Loop sync
	//for {
	//	res, err = cli.SyncRequest(30000, res.NextBatch, "", false, "online")
	//	if err != nil {
	//		time.Sleep(10)
	//		continue
	//	}
	//	Filter(res, roomID)
	//	break
	//}
}
