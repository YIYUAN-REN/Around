package main

import (
	elastic "gopkg.in/olivere/elastic.v3"

	"encoding/json"
	"fmt"
	"net/http"
	"reflect"
	"regexp"
	"time"

	"github.com/dgrijalva/jwt-go"
)

const (
	TYPE_USER = "user" // 往elastic search保存用户信息使用
)

var (
	usernamePattern = regexp.MustCompile(`^[a-z0-9_]+$`).MatchString // 正则表达式：判断username是否规范（'^': 从头开始匹配，'$'：匹配到末尾，'[...]': 可以是括号内任何字符, '+': 一个或多个）
)

type User struct {
	Username string `json:"username"`
	Password string `json:"password"`
	//Age int `json:"age"`
	//Gender string `json:"gender"`
}

// 检查旧用户的用户名是否合法
func checkUser(username, password string) bool {
	es_client, err := elastic.NewClient(elastic.SetURL(ES_URL), elastic.SetSniff(false)) // 创建elastic search的handle
	if err != nil {
		fmt.Printf("ES is not setup %v\n", err)
		return false
	}

	// Search with a term(关键词) query，(类似TicketMaster的search)
	termQuery := elastic.NewTermQuery("username", username)
	queryResult, err := es_client.Search().
		Index(INDEX).
		Query(termQuery).
		Pretty(true).
		Do() // 发送请求
	if err != nil {
		fmt.Printf("ES query failed %v\n", err)
		return false
	}

	var tyu User
	for _, item := range queryResult.Each(reflect.TypeOf(tyu)) { // reflect找到user type（其实只会循环一次，循环方便把item取出）
		u := item.(User)
		return u.Password == password && u.Username == username // 比较password及username和传进来的是否一样
	}
	// If no user exist, return false.
	return false
}

// 添加新用户，成功返回true
func addUser(user User) bool {
	es_client, err := elastic.NewClient(elastic.SetURL(ES_URL), elastic.SetSniff(false))
	if err != nil {
		fmt.Printf("ES is not setup %v\n", err)
		return false
	}

	// 检查用户是否已经存在
	termQuery := elastic.NewTermQuery("username", user.Username)
	queryResult, err := es_client.Search().
		Index(INDEX).
		Query(termQuery).
		Pretty(true).
		Do()
	if err != nil {
		fmt.Printf("ES query failed %v\n", err)
		return false
	}

	if queryResult.TotalHits() > 0 { // Total Hits大于0次，则该用户名已被注册
		fmt.Printf("User %s already exists, cannot create duplicate user.\n", user.Username)
		return false
	}

	// 上面判断过后说明用户不存在，可以往elastic search插入用户信息
	_, err = es_client.Index().
		Index(INDEX).
		Type(TYPE_USER).
		Id(user.Username).
		BodyJson(user).
		Refresh(true). // 若重名就刷新，其实不用只是为了保险
		Do()
	if err != nil {
		fmt.Printf("ES save user failed %v\n", err)
		return false
	}

	return true
}

// 若注册成功，创建新token，返回成功说明
func signupHandler(w http.ResponseWriter, r *http.Request) {
	fmt.Println("Received one signup request") // 表明进入函数

	// 解码
	decoder := json.NewDecoder(r.Body) // 判断json读取是否出错，当然前端会保证它正确（signup传的是json(username,password,age,gender)，decode这个json）
	var u User
	if err := decoder.Decode(&u); err != nil {
		panic(err)
		return
	}

	// 若密码存在 and 用户名存在且合法，则创建user
	if u.Username != "" && u.Password != "" && usernamePattern(u.Username) {
		if addUser(u) {
			fmt.Println("User added successfully.")     // 打印成功
			w.Write([]byte("User added successfully.")) // 客户端返回成功，双保险（网络传输一定要用byte）
		} else {
			fmt.Println("Failed to add a new user.")
			http.Error(w, "Failed to add a new user", http.StatusInternalServerError)
		}
	} else {
		fmt.Println("Empty password or username.")
		http.Error(w, "Empty password or username", http.StatusInternalServerError)
	}

	w.Header().Set("Content-Type", "text/plain")       // 返回结果是文本
	w.Header().Set("Access-Control-Allow-Origin", "*") // 所有人都可访问
}

// 若登录成功，创建新token，返回token
func loginHandler(w http.ResponseWriter, r *http.Request) {
	fmt.Println("Received one login request")

	decoder := json.NewDecoder(r.Body)
	var u User
	if err := decoder.Decode(&u); err != nil {
		panic(err)
		return
	}

	// 检查用户名密码和elastic search里存的用户名密码是否匹配
	// 若匹配成功，则生成token发给用户
	if checkUser(u.Username, u.Password) {
		token := jwt.New(jwt.SigningMethodHS256) // jwt生成token
		claims := token.Claims.(jwt.MapClaims)   // 取出claim(默认interface格式)转化成map格式(go可用)，(claim就是HEADER.PAYLOAD.VERIFY SIGNITURE中的PAYLOAD)
		/* Set token claims */
		claims["username"] = u.Username
		claims["exp"] = time.Now().Add(time.Hour * 24).Unix() // claim写进过期时间

		/* Sign the token with our secret */
		tokenString, _ := token.SignedString(mySigningKey) // 用token sign一下string(sign string过程 = 生成VERIFY SIGNITURE过程)

		/* Finally, write the token to the browser window */
		w.Write([]byte(tokenString)) //（网络传输一定要用byte）
	} else {
		fmt.Println("Invalid password or username.")
		http.Error(w, "Invalid password or username", http.StatusForbidden)
	}

	w.Header().Set("Content-Type", "text/plain")
	w.Header().Set("Access-Control-Allow-Origin", "*")
}
