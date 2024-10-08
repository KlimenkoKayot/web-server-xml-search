package main

import (
	"encoding/json"
	"encoding/xml"
	"fmt"
	"io"
	"io/ioutil"
	"net/http"
	"net/http/httptest"
	"sort"
	"strconv"
	"strings"
	"testing"
)

type Person struct {
	Id        int `xml:"id"`
	Name      string
	FirstName string `xml:"first_name"`
	LastName  string `xml:"last_name"`
	Age       int    `xml:"age"`
	About     string `xml:"about"`
	Gender    string `xml:"gender"`
}

type Data struct {
	Persons             []Person `xml:"row"`
	SearchErrorResponse SearchErrorResponse
}

// Разархивация xml в нормальные данные
func UnmarshalXmlFile(path string) (Data, error) {
	dataXml, err := ioutil.ReadFile(path)
	if err != nil {
		return Data{}, fmt.Errorf("[UnmarshalXmlFile] Bad xml data-file: %e", err)
	}
	data := Data{}
	err = xml.Unmarshal(dataXml, &data)
	if err != nil {
		return Data{}, fmt.Errorf("[UnmarshalXmlFile] Cant unmarshall data-file: %e", err)
	}
	for i := 0; i < len(data.Persons); i++ {
		data.Persons[i].Name = data.Persons[i].FirstName + data.Persons[i].LastName
	}
	return data, nil
}

// Поиск по полям Name и About
func (data *Data) FindByStr(substr string) {
	if substr == "" {
		return
	}
	newData := Data{}
	for _, now := range data.Persons {
		if strings.Contains(now.Name, substr) || strings.Contains(now.About, substr) {
			newData.Persons = append(newData.Persons, now)
		}
	}
	data = &newData
}

// Метод Sort для данных по order_field и order_by
func (data *Data) Sort(order_field string, order_by int) error {
	switch order_field {
	case "":
		order_field = "Name"
		fallthrough
	case "Name":
		sort.Slice(data.Persons, func(i, j int) bool {
			if order_by == OrderByDesc {
				return data.Persons[i].Name > data.Persons[j].Name
			}
			return data.Persons[i].Name < data.Persons[j].Name
		})
	case "Id":
		sort.Slice(data.Persons, func(i, j int) bool {
			if order_by == OrderByDesc {
				return data.Persons[i].Id > data.Persons[j].Id
			}
			return data.Persons[i].Id < data.Persons[j].Id
		})
	case "Age":
		sort.Slice(data.Persons, func(i, j int) bool {
			if order_by == OrderByDesc {
				return data.Persons[i].Age > data.Persons[j].Age
			}
			return data.Persons[i].Age < data.Persons[j].Age
		})
	default:
		return fmt.Errorf("ErrorBadOrderField")
	}
	return nil
}

func SearchErrorJson(err string) []byte {
	respError := SearchErrorResponse{
		Error: err,
	}
	respErrorJson, _ := json.Marshal(respError)
	return respErrorJson
}

func SearchServer(w http.ResponseWriter, r *http.Request) {
	// Получаем data из xml-файла
	data, err := UnmarshalXmlFile("dataset.xml")
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		return
	}

	// Получаю параметр из URL запроса и вызываю метод
	query := r.URL.Query().Get("query")
	data.FindByStr(query)

	// Тоже самое
	order_field := r.URL.Query().Get("order_field")
	order_by, err := strconv.Atoi(r.URL.Query().Get("order_by"))
	if err != nil || order_by > 1 || order_by < -1 {
		w.WriteHeader(http.StatusBadRequest)
		io.WriteString(w, string(SearchErrorJson("BadOrderByValue")))
		return
	}
	// Вызвал обработчик
	err = data.Sort(order_field, order_by)
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		io.WriteString(w, string(SearchErrorJson(err.Error())))
		return
	}

	// Вот тут я не совсем понимаю как должны работать limit и offset
	// особенно с параметром NextPage в client.go, потому что он увеличивает
	// на +1 и это (лично мне) сильно мешает
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	offset, _ := strconv.Atoi(r.URL.Query().Get("offset"))
	if limit == 1 { // Непонятно limit == 0 до FindUsers или
		// Параметр не был задан вообще
		limit = len(data.Persons)
	}
	if offset > len(data.Persons) {
		w.WriteHeader(http.StatusInternalServerError)
		io.WriteString(w, string(SearchErrorJson("offset > len(data)")))
		return
	}
	data.Persons = data.Persons[offset:limit]

	toSend, err := json.Marshal(data.Persons)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusOK)
	io.WriteString(w, string(toSend))
}

func TokenChecker(w http.ResponseWriter, r *http.Request) {
	token := r.Header.Get("AccessToken") // Забираю AcccessToken
	if token == adminToken {             // Проверка (adminToken придумал рандомный)
		w.WriteHeader(http.StatusOK)
	} else {
		w.WriteHeader(http.StatusUnauthorized)
	}
}

func MakeRequest(w http.ResponseWriter, r *http.Request) {
	// Запрос в handler для проверки доступа AccessToken
	recChecker := httptest.NewRecorder()          // Новый рекордер, чтобы не портить старый
	handChecker := http.HandlerFunc(TokenChecker) // Задаю handler функцию
	handChecker.ServeHTTP(recChecker, r)          // Запрос в hadnler

	if recChecker.Result().StatusCode != http.StatusOK {
		// Если пришел плохой статус, возвращаю его
		// и закрываю нынешний handler
		w.WriteHeader(http.StatusUnauthorized)
		return
	}

	recSearchServer := httptest.NewRecorder()          // Опять новый рекордер
	handSearchServer := http.HandlerFunc(SearchServer) // handler функция
	handSearchServer.ServeHTTP(recSearchServer, r)     // Запрос в handler

	w.WriteHeader(recSearchServer.Result().StatusCode) // Возвращаю результат
	io.WriteString(w, recSearchServer.Body.String())   // ..
}

type TestCase struct {
	SearchClient        SearchClient        // Структура с токеном и ссылкой на сервис
	SearchRequest       SearchRequest       // Запрос
	SearchErrorResponse SearchErrorResponse // Ошибка
	IsError             bool                // Для дебага ошибок
	Result              string              // Результат теста
}

func TestSearchServer(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(MakeRequest))
	TestCases := []TestCase{
		{
			SearchClient: SearchClient{
				AccessToken: "123356",
				URL:         ts.URL,
			},
			SearchRequest: SearchRequest{
				Query: "o",
			},
			IsError: false,
		},
		{
			SearchClient: SearchClient{
				AccessToken: "123356",
				URL:         ts.URL,
			},
			SearchRequest: SearchRequest{
				Limit: -1,
			},
			IsError: true,
		},
		{
			SearchClient: SearchClient{
				AccessToken: "123356",
				URL:         ts.URL,
			},
			SearchRequest: SearchRequest{
				Limit: 27,
			},
			IsError: false,
		},
		{
			SearchClient: SearchClient{
				AccessToken: "123356",
				URL:         ts.URL,
			},
			SearchRequest: SearchRequest{
				Offset: -1,
			},
			IsError: true,
		},
		{
			SearchClient: SearchClient{
				AccessToken: "12335",
				URL:         ts.URL,
			},
			SearchRequest: SearchRequest{},
			IsError:       true,
		},
		{
			SearchClient: SearchClient{
				AccessToken: "123356",
				URL:         ts.URL,
			},
			SearchRequest: SearchRequest{
				OrderBy: 135531,
			},
			IsError: true,
		},
		{
			SearchClient: SearchClient{
				AccessToken: "123356",
				URL:         ts.URL,
			},
			SearchRequest: SearchRequest{
				OrderField: "Aboba",
			},
			IsError: true,
		},
		{
			SearchClient: SearchClient{
				AccessToken: "123356",
				URL:         ts.URL,
			},
			SearchRequest: SearchRequest{
				Query:   "agdgdewg",
				Offset:  1356,
				OrderBy: OrderByDesc,
				Limit:   2,
			},
			IsError: true,
		},
		{
			SearchClient: SearchClient{
				AccessToken: "123356",
			},
			SearchRequest: SearchRequest{
				Query: "adg",
			},
			IsError: true,
		},
	}

	for testNum, testCase := range TestCases {
		// TokenChecker & SearchServer
		// ..
		// Я обобщил в MakeRequest, т.к не знаю как использовать
		// метод FindUsers так, чтобы сначала была обработка по
		// AccessToken, а затем происходил SearchServer

		// Проверку результата еще не делал, чтобы код был красивым
		// и читабельным
		_, err := testCase.SearchClient.FindUsers(testCase.SearchRequest)
		if err != nil && !testCase.IsError {
			t.Errorf("[%d] unexpected error: %#v", testNum, err)
		} else if err == nil && testCase.IsError {
			t.Errorf("[%d] expected error, got nil: %#v", testNum, err)
		}
	}
	ts.Close()
}
