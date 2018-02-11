package qldb

import (
	"bytes"
	"encoding/gob"
	"errors"
	"log"
	"runtime"
	"strconv"
	"strings"

	"github.com/awgh/bencrypt/bc"
	"github.com/awgh/ratnet/api"
)

// ID : Return routing key
func (node *Node) ID() (bc.PubKey, error) {
	return node.routingKey.GetPubKey(), nil
}

// Dropoff : Deliver a batch of  messages to a remote node
func (node *Node) Dropoff(bundle api.Bundle) error {
	node.debugMsg("Dropoff called")
	if len(bundle.Data) < 1 { // todo: correct min length
		return errors.New("Dropoff called with no data")
	}
	tagOK, data, err := node.routingKey.DecryptMessage(bundle.Data)
	if err != nil {
		return err
	} else if !tagOK {
		return errors.New("Luggage Tag Check Failed in Dropoff")
	}
	var msgs [][]byte

	//Use default gob decoder
	reader := bytes.NewReader(data)
	dec := gob.NewDecoder(reader)
	if err := dec.Decode(&msgs); err != nil {
		log.Printf("dropoff gob decode failed, len %d\n", len(data))
		return err
	}
	for i := 0; i < len(msgs); i++ {
		if len(msgs[i]) < 16 { // aes.BlockSize == 16
			continue //todo: remove padding before here?
		}
		err = node.router.Route(node, msgs[i])
		if err != nil {
			log.Println("error in dropoff: " + err.Error())
			continue // we don't want to return routing errors back out the remote public interface
		}
	}
	node.debugMsg("Dropoff returned")
	return nil
}

/* todo: when multiple profiles enabled at once is implemented, switch to the below (or similar):
profiles, err := node.GetProfiles()
if err != nil {
	node.handleErr(err)
	continue
}
for _, profile := range profiles {
	if profile.Enabled {
		clearMsg.Name = profile.Name
		break
	}
}
*/

// Pickup : Get messages from a remote node
func (node *Node) Pickup(rpub bc.PubKey, lastTime int64, channelNames ...string) (api.Bundle, error) {
	node.debugMsg("Pickup called")
	c := node.db()
	var retval api.Bundle
	wildcard := false
	if len(channelNames) < 1 {
		wildcard = true // if no channels are given, get everything
	} else {
		for _, cname := range channelNames {
			for _, char := range cname {
				if !strings.Contains("abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0987654321", string(char)) {
					return retval, errors.New("Invalid character in channel name")
				}
			}
		}
	}
	sqlq := "SELECT msg, timestamp FROM outbox"
	if lastTime != 0 {
		sqlq += " WHERE (int64(" + strconv.FormatInt(lastTime, 10) +
			") < timestamp)"
	}
	if !wildcard && len(channelNames) > 0 { // QL is broken?  couldn't make it work with prepared stmts
		if lastTime != 0 {
			sqlq += " AND"
		} else {
			sqlq += " WHERE"
		}
		sqlq = sqlq + " channel IN( \"" + channelNames[0] + "\""
		for i := 1; i < len(channelNames); i++ {
			sqlq = sqlq + ",\"" + channelNames[i] + "\""
		}
		sqlq = sqlq + " )"
	}
	// todo:  ORDER BY breaks on android/arm and returns nothing without error, report to cznic
	sqlq = sqlq + " ORDER BY timestamp ASC LIMIT 250;"
	//sqlq = sqlq;"

	runtime.GC()
	r := transactQuery(c, sqlq)

	var msgs [][]byte
	var msg []byte
	var ts int64
	lastTimeReturned := lastTime
	for r.Next() {
		r.Scan(&msg, &ts)
		if ts > lastTimeReturned { // do this instead of ORDER BY, for android
			lastTimeReturned = ts
		} else {
			log.Printf("Timestamps not increasing - prev: %d  cur: %d\n", lastTimeReturned, ts)
		}
		//log.Printf("ts: %d\n", ts)
		msgs = append(msgs, msg)
	}
	r.Close()

	//log.Printf("rows returned by Pickup query: %d, lastTime: %d\n", len(msgs), lastTimeReturned)
	retval.Time = lastTimeReturned
	if len(msgs) > 0 {
		//use default gob encoder
		var buf bytes.Buffer
		enc := gob.NewEncoder(&buf)
		if err := enc.Encode(msgs); err != nil {
			return retval, err
		}
		cipher, err := node.routingKey.EncryptMessage(buf.Bytes(), rpub)
		if err != nil {
			log.Printf("pickup gob encode failed, len %d\n", len(cipher))
			return retval, err
		}
		retval.Data = cipher

		msgs = nil
		return retval, err
	}
	node.debugMsg("Pickup returned")
	return retval, nil
}
