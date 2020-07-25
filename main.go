package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"reflect"
	"strconv"

	elastic "gopkg.in/olivere/elastic.v3"

	"context"
	"io"

	"cloud.google.com/go/storage"
	"github.com/pborman/uuid" // uuid：保证每个id都是unique

	jwtmiddleware "github.com/auth0/go-jwt-middleware"
	"github.com/dgrijalva/jwt-go"
	"github.com/gorilla/mux"
)

// (相当于servlet, struct相当于class)
type Location struct {
	Lat float64 `json:"lat"` // 告诉decoder，Go里的大写Lat(大写相当于public，小写相当于private)对应Json里的小写lat
	Lon float64 `json:"lon"`
}

// (相当于servlet)
type Post struct {
	User     string   `json:"user"`
	Message  string   `json:"message"`
	Location Location `json:"location"`
	Url      string   `json:"url"`
}

const ( // const相当于final
	INDEX    = "around" // 表示around这个project，其他也可以是jupiter等等
	TYPE     = "post"
	DISTANCE = "200km"
	// Needs to update
	//PROJECT_ID = "around-xxx"
	//BT_INSTANCE = "around-post"
	// Needs to update this URL if you deploy it to cloud.
	ES_URL = "http://104.155.165.168:9200"

	// Needs to update this bucket based on your gcs bucket name.
	BUCKET_NAME = "post-images-284203"
)

var mySigningKey = []byte("secret") // signing key

func main() {
	// Create a client, 往后每个endpoint都要创建新client，为了保持一个连接
	client, err := elastic.NewClient(elastic.SetURL(ES_URL), elastic.SetSniff(false))
	if err != nil {
		panic(err)
		return
	}

	// Use the IndexExists service to check if a specified index exists.
	exists, err := client.IndexExists(INDEX).Do() // INDEX是around
	if err != nil {
		panic(err)
	}
	if !exists {
		// Create a new index. (第一次mapping)
		mapping := `{
			"mappings":{
				"post":{
					"properties":{
						"location":{
							"type":"geo_point"
						}
					}
				}
			}
		}`
		_, err := client.CreateIndex(INDEX).Body(mapping).Do()
		if err != nil {
			// Handle error
			panic(err)
		}
	}

	// 启动service
	fmt.Println("started-service")

	// Here we are instantiating the gorilla/mux router
	r := mux.NewRouter() //

	var jwtMiddleware = jwtmiddleware.New(jwtmiddleware.Options{
		ValidationKeyGetter: func(token *jwt.Token) (interface{}, error) { // 从JWT token里拿到key
			return mySigningKey, nil
		},
		SigningMethod: jwt.SigningMethodHS256,
	})

	r.Handle("/post", jwtMiddleware.Handler(http.HandlerFunc(handlerPost))).Methods("POST") // 用router来handle请求，jwtMiddleware加在中间目的是验证(相当于token到server后验证): 验证router和signing key能否对上，若能对上再转交给http handler
	r.Handle("/search", jwtMiddleware.Handler(http.HandlerFunc(handlerSearch))).Methods("GET")
	r.Handle("/login", http.HandlerFunc(loginHandler)).Methods("POST") // 为什么不用jwtMiddleware：用户需输入账户密码，token还没产生
	r.Handle("/signup", http.HandlerFunc(signupHandler)).Methods("POST")

	http.Handle("/", r) // 把router返回
	log.Fatal(http.ListenAndServe(":8080", nil))

	http.HandleFunc("/post", handlerPost) // 呼叫endpoint(相当于servlet里的doPost，这里函数就可以实现doPost功能)
	http.HandleFunc("/search", handlerSearch)
	/*				endpoint	保存endpoint用的函数					*/
	log.Fatal(http.ListenAndServe(":8080", nil)) // Fatal里面（把http服务跑在8080端口，nil是因为我们已有handler不用再定义handler）正常执行。若前面出错，则打印错误日志退出(类似Java里的try catch)
}

// handle POST的请求：把用户请求从http.Request读出
/*
http request的Json格式：
{
	"user_name": "John",
	"message": "Test",
	"location": {
		"lat": 37,
		"lon": -120
	}
}
*/
func handlerPost(w http.ResponseWriter, r *http.Request) {
	fmt.Println("Receive one post request.") // 先提示用户表示进入函数

	/*
		// decoder获得Json格式的string，再decode成Go的数据结构
		decoder := json.NewDecoder(r.Body) // (r.Body相当于*r.body)
		var p Post
		if err := decoder.Decode(&p); err != nil { // 若error，抛出异常。(';'表示两个statments --- 初始化+判断), (panic相当于Java的throw), (Decode直接在p上修改，返回error或者no error)
			panic(err)
		}
	*/

	// 不用JSON而是用 multipart form

	/* 1. 用于前端 */
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Access-Control-Allow-Headers", "Content-Type,Authorization")

	/* 2. parse form data */
	// 从token读出用户名
	user := r.Context().Value("user")
	claims := user.(*jwt.Token).Claims
	username := claims.(jwt.MapClaims)["username"]

	r.ParseMultipartForm(32 << 20) // 提交form的最大32 MB

	fmt.Printf("Received one post request %s\n", r.FormValue("message")) // 打印检测到的数据
	// 解析lat和lon
	lat, _ := strconv.ParseFloat(r.FormValue("lat"), 64)
	lon, _ := strconv.ParseFloat(r.FormValue("lon"), 64)
	// 解析message + 重新拼一下
	p := &Post{ // 为了防止拷贝
		User:    username.(string),
		Message: r.FormValue("message"),
		Location: Location{
			Lat: lat,
			Lon: lon,
		},
	}

	id := uuid.New()

	// 解析image
	file, _, err := r.FormFile("image")
	if err != nil {
		http.Error(w, "Image is not available", http.StatusInternalServerError) // (也可以用panic)
		fmt.Printf("Image is not available %v.\n", err)
		return
	}
	defer file.Close()

	/* 3. GCS */
	ctx := context.Background()

	// replace it with your real bucket name.
	_, attrs, err := saveToGCS(ctx, file, BUCKET_NAME, id) // attrs里面有提交到GCS后创建的URL
	if err != nil {
		http.Error(w, "GCS is not setup", http.StatusInternalServerError)
		fmt.Printf("GCS is not setup %v\n", err)
		return
	}

	// Update the media link after saving to GCS.
	p.Url = attrs.MediaLink

	saveToES(p, id) // save to ES，p是指针

}

func saveToGCS(ctx context.Context, r io.Reader, bucketName, name string) (*storage.ObjectHandle, *storage.ObjectAttrs, error) {
	// 1. client用来操作bucket
	client, err := storage.NewClient(ctx)
	if err != nil {
		return nil, nil, err
	}
	defer client.Close()

	// 2. 创建bucket
	bucket := client.Bucket(bucketName)
	// Next check if the bucket exists
	if _, err = bucket.Attrs(ctx); err != nil {
		return nil, nil, err
	}

	obj := bucket.Object(name) //文件名是uuid
	w := obj.NewWriter(ctx)
	if _, err := io.Copy(w, r); err != nil {
		return nil, nil, err
	}
	if err := w.Close(); err != nil {
		return nil, nil, err
	}

	// 文件写完没有人能读，所以修改权限
	if err := obj.ACL().Set(ctx, storage.AllUsers, storage.RoleReader); err != nil {
		return nil, nil, err
	}

	attrs, err := obj.Attrs(ctx) // 获取刚创建文件的属性
	fmt.Printf("Post is saved to GCS: %s\n", attrs.MediaLink)
	return obj, attrs, err
}

func saveToES(p *Post, id string) {
	// Create a client
	es_client, err := elastic.NewClient(elastic.SetURL(ES_URL), elastic.SetSniff(false))
	if err != nil {
		panic(err)
		return
	}

	// Save it to index
	_, err = es_client.Index().
		Index(INDEX).
		Type(TYPE). // TYPE为post
		Id(id).     // uuid生成的id
		BodyJson(p).
		Refresh(true). // 如果有新id就新把旧替换(有uuid一般不会出现重复)
		Do()           // 输入到elastic search
	if err != nil {
		panic(err)
		return
	}

	fmt.Printf("Post is saved to Index: %s\n", p.Message)
}

// handle附近POST的结果
// 获取参数lat, lon, ran, 用假post转成string返回
func handlerSearch(w http.ResponseWriter, r *http.Request) {
	fmt.Println("Received one request for search.") // 先告诉用户收到结果了

	// 从GET request(URL)里把参数取出
	lat, _ := strconv.ParseFloat(r.URL.Query().Get("lat"), 64) // r.URL.Query().Get("lat")是string，转成float64
	lon, _ := strconv.ParseFloat(r.URL.Query().Get("lon"), 64) // '_'表示忽略返回err

	// 检查传进来的URL有没有range参数，没有的话用默认值
	ran := DISTANCE
	if val := r.URL.Query().Get("range"); val != "" {
		ran = val + "km"
	}

	fmt.Printf("Search received: %f %f %s\n", lat, lon, ran)

	// create a client, 用Go的elastic API操作远程elastic search服务
	client, err := elastic.NewClient(elastic.SetURL(ES_URL), elastic.SetSniff(false)) // (SetSniff: 回调函数来记录状态，这次我们不需要)
	if err != nil {
		panic(err)
	}

	// 有client后，进行搜索
	q := elastic.NewGeoDistanceQuery("location") // query名字叫location
	q = q.Distance(ran).Lat(lat).Lon(lon)

	searchResult, err := client.Search().
		Index(INDEX).
		Query(q).
		Pretty(true). // 让返回的Json好看一点
		Do()          // 进行搜索(前面几个'.'都是设置参数)
	if err != nil {
		panic(err)
	}

	fmt.Println("Query took %d milliseconds\n", searchResult.TookInMillis) // 多长时间
	fmt.Printf("Found a total of %d posts\n", searchResult.TotalHits())    // 多少个结果

	// 拿到结果(searchResult)后，返回到之前POST的类型
	var typ Post
	var ps []Post
	for _, item := range searchResult.Each(reflect.TypeOf(typ)) { // 反射获取POST类型,（相当于Java instance of)
		p := item.(Post) // 从item里获得POST类型，(相当于Java的类型转换 p = (Post) item)
		fmt.Printf("Post by %s: %s at lat %v and lon %v\n",
			p.User, p.Message, p.Location.Lat, p.Location.Lon)

		ps = append(ps, p) // p是获得的POST类型，加到ps即POST slice，ps是搜到的所有结果
	}

	// 把POST slice转成Json string输出给客户
	js, err := json.Marshal(ps) // (json.Marshal：把上面Go的数据结构转化成string)
	if err != nil {
		panic(err)
	}

	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Access-Control-Allow-Origin", "*") // ('*'：任何脚本位置)
	w.Write(js)

	/*
		fmt.Println("range is ", ran)

		// return fake post (目的：利用Json返回机制，直接生成一个结果)
		p := &Post{ // ('&': 下面的p就不用写成&p，而且传指针可以避免拷贝)
			User:    "1111",
			Message: "一生必须的100个地方",
			Location: Location{
				Lat: lat,
				Lon: lon,
			},
		}

		js, err := json.Marshal(p) // (json.Marshal：把上面Go的数据结构转化成string)
		if err != nil {
			panic(err)
		}

		w.Header().Set("Content-Type", "application/json")
		w.Write(js)

		// fmt.Fprintf(w, "Search received: %s %s", lat, lon)
	*/
}
