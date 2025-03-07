package transport

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/golang/mock/gomock"
	"github.com/influxdata/influxdb/v2/kit/feature"
	"github.com/influxdata/influxdb/v2/kit/platform"
	"github.com/influxdata/influxdb/v2/notebooks/service"
	"github.com/influxdata/influxdb/v2/notebooks/service/mocks"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest"
)

var (
	orgStr       = "1234123412341234"
	orgID, _     = platform.IDFromString(orgStr)
	idStr        = "4321432143214321"
	id, _        = platform.IDFromString(idStr)
	testNotebook = &service.Notebook{
		OrgID: *orgID,
		ID:    *id,
		Name:  "test notebook",
		Spec: service.NotebookSpec{
			"hello": "goodbye",
		},
	}
	testReqBody = &service.NotebookReqBody{
		OrgID: *orgID,
		Name:  "Test notebook",
		Spec: service.NotebookSpec{
			"hello": "goodbye",
		},
	}
)

func TestNotebookHandler(t *testing.T) {
	t.Parallel()

	t.Run("get notebooks happy path", func(t *testing.T) {
		ts, svc := newTestServer(t)
		defer ts.Close()

		req := newTestRequest(t, "GET", ts.URL, nil)

		q := req.URL.Query()
		q.Add("orgID", orgStr)
		req.URL.RawQuery = q.Encode()

		svc.EXPECT().
			ListNotebooks(gomock.Any(), service.NotebookListFilter{OrgID: *orgID}).
			Return([]*service.Notebook{testNotebook}, nil)

		res := doTestRequest(t, req, http.StatusOK, true)

		got := []*service.Notebook{}
		err := json.NewDecoder(res.Body).Decode(&got)
		require.NoError(t, err)
		require.Equal(t, got, []*service.Notebook{testNotebook})
	})

	t.Run("create notebook happy path", func(t *testing.T) {
		ts, svc := newTestServer(t)
		defer ts.Close()

		req := newTestRequest(t, "POST", ts.URL, testReqBody)

		svc.EXPECT().
			CreateNotebook(gomock.Any(), testReqBody).
			Return(testNotebook, nil)

		res := doTestRequest(t, req, http.StatusOK, true)

		got := &service.Notebook{}
		err := json.NewDecoder(res.Body).Decode(got)
		require.NoError(t, err)
		require.Equal(t, got, testNotebook)
	})

	t.Run("get notebook happy path", func(t *testing.T) {
		ts, svc := newTestServer(t)
		defer ts.Close()

		req := newTestRequest(t, "GET", ts.URL+"/"+idStr, nil)

		svc.EXPECT().
			GetNotebook(gomock.Any(), *id).
			Return(testNotebook, nil)

		res := doTestRequest(t, req, http.StatusOK, true)

		got := &service.Notebook{}
		err := json.NewDecoder(res.Body).Decode(got)
		require.NoError(t, err)
		require.Equal(t, got, testNotebook)
	})

	t.Run("delete notebook happy path", func(t *testing.T) {
		ts, svc := newTestServer(t)
		defer ts.Close()

		req := newTestRequest(t, "DELETE", ts.URL+"/"+idStr, nil)

		svc.EXPECT().
			DeleteNotebook(gomock.Any(), *id).
			Return(nil)

		doTestRequest(t, req, http.StatusNoContent, false)
	})

	t.Run("update notebook happy path", func(t *testing.T) {
		ts, svc := newTestServer(t)
		defer ts.Close()

		req := newTestRequest(t, "PUT", ts.URL+"/"+idStr, testReqBody)

		svc.EXPECT().
			UpdateNotebook(gomock.Any(), *id, testReqBody).
			Return(testNotebook, nil)

		res := doTestRequest(t, req, http.StatusOK, true)

		got := &service.Notebook{}
		err := json.NewDecoder(res.Body).Decode(got)
		require.NoError(t, err)
		require.Equal(t, got, testNotebook)
	})

	t.Run("invalid notebook ids return 400", func(t *testing.T) {
		methodsWithBody := []string{"PATCH", "PUT"}
		methodsNoBody := []string{"GET", "DELETE"}

		for _, m := range methodsWithBody {
			t.Run(m+" /notebooks", func(t *testing.T) {
				ts, _ := newTestServer(t)
				defer ts.Close()

				req := newTestRequest(t, m, ts.URL+"/badid", testReqBody)
				doTestRequest(t, req, http.StatusBadRequest, false)
			})
		}

		for _, m := range methodsNoBody {
			t.Run(m+" /notebooks", func(t *testing.T) {
				ts, _ := newTestServer(t)
				defer ts.Close()

				req := newTestRequest(t, m, ts.URL+"/badid", nil)
				doTestRequest(t, req, http.StatusBadRequest, false)
			})
		}
	})

	t.Run("invalid org id to GET /notebooks returns 400", func(t *testing.T) {
		ts, _ := newTestServer(t)
		defer ts.Close()

		req := newTestRequest(t, "GET", ts.URL, nil)

		q := req.URL.Query()
		q.Add("orgID", "badid")
		req.URL.RawQuery = q.Encode()

		doTestRequest(t, req, http.StatusBadRequest, false)
	})

	t.Run("invalid request body returns 400", func(t *testing.T) {
		badBady := &service.NotebookReqBody{
			OrgID: *orgID,
		}

		methods := []string{"PUT", "PATCH"}
		for _, m := range methods {
			t.Run(m+"/notebooks/{id]", func(t *testing.T) {
				ts, _ := newTestServer(t)
				defer ts.Close()

				req := newTestRequest(t, m, ts.URL+"/"+idStr, badBady)
				doTestRequest(t, req, http.StatusBadRequest, false)
			})
		}

		t.Run("POST /notebooks", func(t *testing.T) {
			ts, _ := newTestServer(t)
			defer ts.Close()

			req := newTestRequest(t, "POST", ts.URL+"/", badBady)
			doTestRequest(t, req, http.StatusBadRequest, false)
		})
	})
}

// The svc generated is returned so that the caller can specify the expected
// use of the mock service.
func newTestServer(t *testing.T) (*httptest.Server, *mocks.MockNotebookService) {
	ctrlr := gomock.NewController(t)
	svc := mocks.NewMockNotebookService(ctrlr)
	// server needs to have a middleware to annotate the request context with the
	// appropriate feature flags while notebooks is still behind a feature flag
	server := annotatedTestServer(NewNotebookHandler(zaptest.NewLogger(t), svc))
	return httptest.NewServer(server), svc
}

func newTestRequest(t *testing.T, method, path string, body interface{}) *http.Request {
	dat, err := json.Marshal(body)
	require.NoError(t, err)

	req, err := http.NewRequest(method, path, bytes.NewBuffer(dat))
	require.NoError(t, err)

	req.Header.Add("Content-Type", "application/json")

	return req
}

func doTestRequest(t *testing.T, req *http.Request, wantCode int, needJSON bool) *http.Response {
	res, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	require.Equal(t, wantCode, res.StatusCode)
	if needJSON {
		require.Equal(t, "application/json; charset=utf-8", res.Header.Get("Content-Type"))
	}
	return res
}

func annotatedTestServer(serv http.Handler) http.Handler {
	notebooksFlag := feature.MakeFlag("", "notebooks", "", true, 0, true)
	notebooksApiFlag := feature.MakeFlag("", "notebooksApi", "", true, 0, true)

	return feature.NewHandler(
		zap.NewNop(),
		feature.DefaultFlagger(),
		[]feature.Flag{notebooksFlag, notebooksApiFlag},
		serv,
	)
}
