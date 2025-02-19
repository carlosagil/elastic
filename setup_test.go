// Copyright 2012-present Oliver Eilhard. All rights reserved.
// Use of this source code is governed by a MIT-license.
// See http://olivere.mit-license.org/license.txt for details.

package elastic

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"math/rand"
	"net/http"
	"os"
	"time"
)

const (
	testIndexName  = "elastic-test"
	testIndexName2 = "elastic-test2"
	testMapping    = `
{
	"settings":{
		"number_of_shards":1,
		"number_of_replicas":0
	},
	"mappings":{
		"_default_": {
			"_all": {
				"enabled": true
			}
		},
		"tweet":{
			"properties":{
				"user":{
					"type":"keyword"
				},
				"message":{
					"type":"text",
					"store": true,
					"fielddata": true
				},
				"tags":{
					"type":"keyword"
				},
				"location":{
					"type":"geo_point"
				},
				"suggest_field":{
					"type":"completion",
					"contexts":[
						{
							"name": "user_name",
							"type": "category"
						}
					]
				}
			}
		},
		"comment":{
			"_parent": {
				"type":	"tweet"
			}
		},
		"order":{
			"properties":{
				"article":{
					"type":"text"
				},
				"manufacturer":{
					"type":"keyword"
				},
				"price":{
					"type":"float"
				},
				"time":{
					"type":"date",
					"format": "YYYY-MM-dd"
				}
			}
		},
		"doctype":{
			"properties":{
				"message":{
					"type":"text",
					"store": true,
					"fielddata": true
				}
			}
		},
		"queries":{
			"properties": {
				"query": {
					"type":	"percolator"
				}
			}
		},
		"tweet-nosource":{
			"_source": {
				"enabled": false
			},
			"properties":{
				"user":{
					"type":"keyword"
				},
				"message":{
					"type":"text",
					"store": true,
					"fielddata": true
				},
				"tags":{
					"type":"keyword"
				},
				"location":{
					"type":"geo_point"
				},
				"suggest_field":{
					"type":"completion",
					"contexts":[
						{
							"name":"user_name",
							"type":"category"
						}
					]
				}
			}
		}
	}
}
`
)

type tweet struct {
	User     string        `json:"user"`
	Message  string        `json:"message"`
	Retweets int           `json:"retweets"`
	Image    string        `json:"image,omitempty"`
	Created  time.Time     `json:"created,omitempty"`
	Tags     []string      `json:"tags,omitempty"`
	Location string        `json:"location,omitempty"`
	Suggest  *SuggestField `json:"suggest_field,omitempty"`
}

func (t tweet) String() string {
	return fmt.Sprintf("tweet{User:%q,Message:%q,Retweets:%d}", t.User, t.Message, t.Retweets)
}

type comment struct {
	User    string    `json:"user"`
	Comment string    `json:"comment"`
	Created time.Time `json:"created,omitempty"`
}

func (c comment) String() string {
	return fmt.Sprintf("comment{User:%q,Comment:%q}", c.User, c.Comment)
}

type order struct {
	Article      string  `json:"article"`
	Manufacturer string  `json:"manufacturer"`
	Price        float64 `json:"price"`
	Time         string  `json:"time,omitempty"`
}

func (o order) String() string {
	return fmt.Sprintf("order{Article:%q,Manufacturer:%q,Price:%v,Time:%v}", o.Article, o.Manufacturer, o.Price, o.Time)
}

// doctype is required for Percolate tests.
type doctype struct {
	Message string `json:"message"`
}

// queries is required for Percolate tests.
type queries struct {
	Query string `json:"query"`
}

func isTravis() bool {
	return os.Getenv("TRAVIS") != ""
}

func travisGoVersion() string {
	return os.Getenv("TRAVIS_GO_VERSION")
}

type logger interface {
	Error(args ...interface{})
	Errorf(format string, args ...interface{})
	Fatal(args ...interface{})
	Fatalf(format string, args ...interface{})
	Fail()
	FailNow()
	Log(args ...interface{})
	Logf(format string, args ...interface{})
}

// strictDecoder returns an error if any JSON fields aren't decoded.
type strictDecoder struct{}

func (d *strictDecoder) Decode(data []byte, v interface{}) error {
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	return dec.Decode(v)
}

var (
	logDeprecations = flag.String("deprecations", "off", "log or fail on deprecation warnings")
	strict          = flag.Bool("strict-decoder", false, "treat missing unknown fields in response as errors")
)

func setupTestClient(t logger, options ...ClientOptionFunc) (client *Client) {
	var err error

	client, err = NewClient(options...)
	if err != nil {
		t.Fatal(err)
	}

	// Use strict JSON decoder (unless a specific decoder has been specified already)
	if *strict {
		if client.decoder == nil {
			client.decoder = &strictDecoder{}
		} else if _, ok := client.decoder.(*DefaultDecoder); ok {
			client.decoder = &strictDecoder{}
		}
	}

	// Log deprecations during tests
	if loglevel := *logDeprecations; loglevel != "off" {
		client.deprecationlog = func(req *http.Request, res *http.Response) {
			for _, warning := range res.Header["Warning"] {
				switch loglevel {
				default:
					t.Logf("[%s] Deprecation warning: %s", req.URL, warning)
				case "fail", "error":
					t.Errorf("[%s] Deprecation warning: %s", req.URL, warning)
				}
			}
		}
	}

	client.DeleteIndex(testIndexName).Do(context.TODO())
	client.DeleteIndex(testIndexName2).Do(context.TODO())

	return client
}

func setupTestClientAndCreateIndex(t logger, options ...ClientOptionFunc) *Client {
	client := setupTestClient(t, options...)

	// Create index
	createIndex, err := client.CreateIndex(testIndexName).Body(testMapping).Do(context.TODO())
	if err != nil {
		t.Fatal(err)
	}
	if createIndex == nil {
		t.Errorf("expected result to be != nil; got: %v", createIndex)
	}

	// Create second index
	createIndex2, err := client.CreateIndex(testIndexName2).Body(testMapping).Do(context.TODO())
	if err != nil {
		t.Fatal(err)
	}
	if createIndex2 == nil {
		t.Errorf("expected result to be != nil; got: %v", createIndex2)
	}

	return client
}

func setupTestClientAndCreateIndexAndLog(t logger, options ...ClientOptionFunc) *Client {
	return setupTestClientAndCreateIndex(t, SetTraceLog(log.New(os.Stdout, "", 0)))
}

func setupTestClientAndCreateIndexAndAddDocs(t logger, options ...ClientOptionFunc) *Client {
	client := setupTestClientAndCreateIndex(t, options...)

	// Add tweets
	tweet1 := tweet{User: "olivere", Message: "Welcome to Golang and Elasticsearch."}
	tweet2 := tweet{User: "olivere", Message: "Another unrelated topic."}
	tweet3 := tweet{User: "sandrae", Message: "Cycling is fun."}
	comment1 := comment{User: "nico", Comment: "You bet."}

	_, err := client.Index().Index(testIndexName).Type("tweet").Id("1").BodyJson(&tweet1).Do(context.TODO())
	if err != nil {
		t.Fatal(err)
	}
	_, err = client.Index().Index(testIndexName).Type("tweet").Id("2").BodyJson(&tweet2).Do(context.TODO())
	if err != nil {
		t.Fatal(err)
	}
	_, err = client.Index().Index(testIndexName).Type("tweet").Id("3").Routing("someroutingkey").BodyJson(&tweet3).Do(context.TODO())
	if err != nil {
		t.Fatal(err)
	}
	_, err = client.Index().Index(testIndexName).Type("comment").Id("1").Parent("3").BodyJson(&comment1).Do(context.TODO())
	if err != nil {
		t.Fatal(err)
	}

	// Add orders
	var orders []order
	orders = append(orders, order{Article: "Apple MacBook", Manufacturer: "Apple", Price: 1290, Time: "2015-01-18"})
	orders = append(orders, order{Article: "Paper", Manufacturer: "Canon", Price: 100, Time: "2015-03-01"})
	orders = append(orders, order{Article: "Apple iPad", Manufacturer: "Apple", Price: 499, Time: "2015-04-12"})
	orders = append(orders, order{Article: "Dell XPS 13", Manufacturer: "Dell", Price: 1600, Time: "2015-04-18"})
	orders = append(orders, order{Article: "Apple Watch", Manufacturer: "Apple", Price: 349, Time: "2015-04-29"})
	orders = append(orders, order{Article: "Samsung TV", Manufacturer: "Samsung", Price: 790, Time: "2015-05-03"})
	orders = append(orders, order{Article: "Hoodie", Manufacturer: "h&m", Price: 49, Time: "2015-06-03"})
	orders = append(orders, order{Article: "T-Shirt", Manufacturer: "h&m", Price: 19, Time: "2015-06-18"})
	for i, o := range orders {
		id := fmt.Sprintf("%d", i)
		_, err = client.Index().Index(testIndexName).Type("order").Id(id).BodyJson(&o).Do(context.TODO())
		if err != nil {
			t.Fatal(err)
		}
	}

	// Flush
	_, err = client.Flush().Index(testIndexName).Do(context.TODO())
	if err != nil {
		t.Fatal(err)
	}
	return client
}

func setupTestClientAndCreateIndexAndAddDocsNoSource(t logger, options ...ClientOptionFunc) *Client {
	client := setupTestClientAndCreateIndex(t, options...)

	// Add tweets
	tweet1 := tweet{User: "olivere", Message: "Welcome to Golang and Elasticsearch."}
	tweet2 := tweet{User: "olivere", Message: "Another unrelated topic."}

	_, err := client.Index().Index(testIndexName).Type("tweet-nosource").Id("1").BodyJson(&tweet1).Do(context.TODO())
	if err != nil {
		t.Fatal(err)
	}
	_, err = client.Index().Index(testIndexName).Type("tweet-nosource").Id("2").BodyJson(&tweet2).Do(context.TODO())
	if err != nil {
		t.Fatal(err)
	}
	// Flush
	_, err = client.Flush().Index(testIndexName).Do(context.TODO())
	if err != nil {
		t.Fatal(err)
	}

	return client
}

var letters = []rune("abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ")

func randomString(n int) string {
	b := make([]rune, n)
	for i := range b {
		b[i] = letters[rand.Intn(len(letters))]
	}
	return string(b)
}
