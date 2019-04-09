/*
 * Copyright (C) 2017 The "MysteriumNetwork/node" Authors.
 *
 * This program is free software: you can redistribute it and/or modify
 * it under the terms of the GNU General Public License as published by
 * the Free Software Foundation, either version 3 of the License, or
 * (at your option) any later version.
 *
 * This program is distributed in the hope that it will be useful,
 * but WITHOUT ANY WARRANTY; without even the implied warranty of
 * MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
 * GNU General Public License for more details.
 *
 * You should have received a copy of the GNU General Public License
 * along with this program.  If not, see <http://www.gnu.org/licenses/>.
 */

package endpoints

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/julienschmidt/httprouter"
	"github.com/mysteriumnetwork/node/core/connection"
	"github.com/mysteriumnetwork/node/identity"
	"github.com/mysteriumnetwork/node/market"
	"github.com/stretchr/testify/assert"
)

type fakeManagerForLocation struct {
	onStatusReturn connection.Status
}

func (fm *fakeManagerForLocation) Connect(consumerID identity.Identity, proposal market.ServiceProposal, options connection.ConnectParams) error {
	return nil
}

func (fm *fakeManagerForLocation) Status() connection.Status {
	return fm.onStatusReturn
}

func (fm *fakeManagerForLocation) Disconnect() error {
	return nil
}

func TestAddRoutesForLocationAddsRoutes(t *testing.T) {
	router := httprouter.New()

	AddRoutesForLocation(router, nil, nil, nil)

	tests := []struct {
		method         string
		path           string
		body           string
		expectedStatus int
		expectedJSON   string
	}{
		{
			http.MethodGet, "/location", "",
			http.StatusOK,
			`{
				"ASN": "62179",
				"City": "Vilnius",
				"Continent": "EU",
				"Country": "LT",
				"IP": "1.2.3.4",
				"ISP": "Telia Lietuva, AB",
				"NodeType": "residential"
			}`,
		},
		{
			http.MethodGet, "/location/2.2.2.2", "",
			http.StatusOK,
			`{
				"ASN": "62179",
				"City": "Vilnius",
				"Continent": "EU",
				"Country": "LT",
				"IP": "2.2.2.2",
				"ISP": "Telia Lietuva, AB",
				"NodeType": "residential"
			}`,
		},
	}

	for _, test := range tests {
		resp := httptest.NewRecorder()
		req := httptest.NewRequest(test.method, test.path, strings.NewReader(test.body))
		router.ServeHTTP(resp, req)
		assert.Equal(t, test.expectedStatus, resp.Code)
		if test.expectedJSON != "" {
			assert.JSONEq(t, test.expectedJSON, resp.Body.String())
		} else {
			assert.Equal(t, "", resp.Body.String())
		}
	}
}
