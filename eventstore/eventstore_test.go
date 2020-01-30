/*
event-reporter - report events to the Cacophony Project API.
Copyright (C) 2018, The Cacophony Project

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU General Public License as published by
the Free Software Foundation, either version 3 of the License, or
(at your option) any later version.

This program is distributed in the hope that it will be useful,
but WITHOUT ANY WARRANTY; without even the implied warranty of
MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
GNU General Public License for more details.

You should have received a copy of the GNU General Public License
along with this program.  If not, see <https://www.gnu.org/licenses/>.
*/

package eventstore

import (
	"encoding/json"
	"io/ioutil"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/boltdb/bolt"
	"github.com/stretchr/testify/suite"
)

type Suite struct {
	suite.Suite

	tempDir string
	store   *EventStore
}

func (suite *Suite) SetupTest() {
	tempDir, err := ioutil.TempDir(os.TempDir(), "eventstore_test")
	suite.Require().NoError(err)
	suite.tempDir = tempDir

	suite.store = suite.openStore()
}

func (suite *Suite) openStore() *EventStore {
	store, err := Open(filepath.Join(suite.tempDir, "store.db"))
	suite.Require().NoError(err)
	return store
}

func (suite *Suite) TearDownTest() {
	if suite.store != nil {
		suite.store.Close()
		suite.store = nil
	}
	if suite.tempDir != "" {
		os.RemoveAll(suite.tempDir)
		suite.tempDir = ""
	}
}

// TestMigrate will add data to the old bucket using the old method then try to
// migrate the data to the new bucket. Will compare results.
func (s *Suite) TestMigrate() {
	err := s.store.db.Update(func(tx *bolt.Tx) error {
		_, err := tx.CreateBucketIfNotExists(oldBucketName)
		return err
	})
	s.NoError(err)

	details := []map[string]interface{}{
		map[string]interface{}{
			"fileId": "bird2",
			"volume": "2",
		},
		map[string]interface{}{
			"fileId": "cat1",
			"volume": "1",
		},
	}
	types := []string{
		"audioBait1",
		"audioBait2",
	}
	times := map[time.Time]int{
		Now():                  0,
		Now().Add(time.Second): 1,
	}

	// Adding some event using the old method
	for t, i := range times {
		eventDetails := map[string]interface{}{
			"description": map[string]interface{}{
				"type":    types[i],
				"details": details[i],
			},
		}
		detailsJSON1, err := json.Marshal(&eventDetails)
		s.NoError(err)
		s.NoError(s.store.Queue(detailsJSON1, t))
	}

	// Close and reopen store, the migration happens when the store is opened so
	// that is why it is closed and opened again
	s.store.Close()
	store2 := s.openStore()
	keys, err := store2.GetKeys()
	s.NoError(err)
	// Check that each event did a proper migration
	for _, key := range keys {
		detailsBytes, err := store2.Get(key)
		event := &Event{}
		s.NoError(json.Unmarshal(detailsBytes, event))
		i := times[event.Timestamp]
		s.Equal(types[i], event.Description.Type) // Checkign that type was properly migrated
		detailsRaw, err := json.Marshal(details[i])
		s.NoError(err)
		s.Equal(string(detailsRaw), event.Description.Details) // Checkign that details was properly migrated
	}

	// Close and open store again checking that the migration doesn't happen twice
	store2.Close()
	store3 := s.openStore()
	eventTimes, err := store3.All()
	s.NoError(err)
	s.Equal(len(eventTimes), 0)
}

func (s *Suite) TestAddAndGet() {
	events := map[Event]struct{}{
		Event{Description: EventDescription{Details: "details1", Type: "type1"}}: {},
		Event{Description: EventDescription{Details: "details2", Type: "type2"}}: {},
		Event{Description: EventDescription{Details: "details3", Type: "type3"}}: {},
	}

	// Test addign data
	for e, _ := range events {
		s.NoError(s.store.Add(&e), "error with adding data")
	}

	// Test GetKeys
	keys, err := s.store.GetKeys()
	s.NoError(err, "error returned when getting all keys")
	s.Equal(len(events), len(keys), "error with number of keys returned")

	// Test deleting and getting data
	deleteKey := keys[0]
	deletedEventBytes, err := s.store.Get(deleteKey)
	s.NoError(err, "error returned when deleting data")
	deletedEvent := &Event{}
	json.Unmarshal(deletedEventBytes, deletedEvent)
	s.NoError(s.store.Delete(deleteKey))
	delete(events, *deletedEvent)
	keys, err = s.store.GetKeys()
	s.NoError(err, "error returned when gettign all keys")

	// Read all keys and check against initial data upload to DB
	for _, key := range keys {
		eventBytes, err := s.store.Get(key)
		s.NoError(err)
		s.NotNil(eventBytes)
		event := &Event{}
		json.Unmarshal(eventBytes, event)
		_, ok := events[*event]
		s.True(ok, "mismatch from data added and in DB")
		delete(events, *event) // Delete data to check that there is no double up
	}
	// There should be no data missed
	s.Equal(0, len(events))
}

func TestRun(t *testing.T) {
	suite.Run(t, new(Suite))
}

func Now() time.Time {
	// Truncate necessary to get rid of monotonic clock reading.
	return time.Now().Truncate(time.Nanosecond)
}
