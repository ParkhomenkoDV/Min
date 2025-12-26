package main

import (
	co "Min/pkg/constants"
	"bufio"
	"fmt"
	"net"
	"strings"
	"sync"
	"time"
)

// Клиент представляет подключенного пользователя
type Client struct {
	conn     net.Conn `doc:"Соединение"`
	nickname string
	room     string `doc:"Комната"`
}

// Глобальные переменные
var (
	clients     = make(map[*Client]bool)
	clientsLock sync.RWMutex
	messages    = make(chan string)
)

func main() {
	fmt.Println("Запуск мессенджера...")

	// Получаем IP адреса машины
	printNetworkInfo()

	// Слушаем на всех интерфейсах: ":8080" или "0.0.0.0:8080"
	listener, err := net.Listen("tcp", ":8080")
	if err != nil {
		fmt.Printf("Ошибка запуска сервера: %v\n", err)
		return
	}
	defer listener.Close()

	// Получаем адрес, на котором запущен сервер
	addr := listener.Addr().(*net.TCPAddr)
	fmt.Printf("Сервер запущен на %s:%d\n", getLocalIP(), addr.Port)
	fmt.Printf("Другие компьютеры могут подключиться по адресу: %s:%d\n", getPublicIP(), addr.Port)
	fmt.Println("Для подключения используйте команду: go run client.go <ваш_ip>:8080")

	// Горутина для рассылки сообщений всем клиентам
	go broadcastMessages()

	for {
		conn, err := listener.Accept()
		if err != nil {
			fmt.Printf("Ошибка подключения: %v\n", err)
			continue
		}

		// Получаем информацию о подключившемся клиенте
		remoteAddr := conn.RemoteAddr().(*net.TCPAddr)
		fmt.Printf("Новое подключение от %s\n", remoteAddr.IP)

		go handleConnection(conn)
	}
}

func printNetworkInfo() {
	// Получаем все сетевые интерфейсы
	interfaces, err := net.Interfaces()
	if err != nil {
		fmt.Println("Не удалось получить сетевые интерфейсы:", err)
		return
	}

	fmt.Println("Доступные сетевые интерфейсы:")
	for _, iface := range interfaces {
		addrs, err := iface.Addrs()
		if err != nil {
			continue
		}

		for _, addr := range addrs {
			ipNet, ok := addr.(*net.IPNet)
			if ok && !ipNet.IP.IsLoopback() && ipNet.IP.To4() != nil {
				fmt.Printf("  - %s: %s\n", iface.Name, ipNet.IP)
			}
		}
	}
}

func getLocalIP() string {
	addrs, err := net.InterfaceAddrs()
	if err != nil {
		return "localhost"
	}

	for _, addr := range addrs {
		ipNet, ok := addr.(*net.IPNet)
		if ok && !ipNet.IP.IsLoopback() && ipNet.IP.To4() != nil {
			return ipNet.IP.String()
		}
	}
	return "localhost"
}

func getPublicIP() string {
	// Можно реализовать получение публичного IP через внешний сервис
	// Но для простоты покажем, как получить локальный IP
	return getLocalIP()
}

func handleConnection(conn net.Conn) {
	defer func() {
		conn.Close()
		fmt.Printf("Соединение закрыто: %s\n", conn.RemoteAddr())
	}()

	// Запрос ника
	conn.Write([]byte("Введите ваш ник: "))
	reader := bufio.NewReader(conn)
	nickname, _ := reader.ReadString('\n')
	nickname = strings.TrimSpace(nickname)

	if nickname == "" {
		nickname = "Аноним"
	}

	client := &Client{
		conn:     conn,
		nickname: nickname,
		room:     "general",
	}

	// Регистрируем клиента
	clientsLock.Lock()
	clients[client] = true
	clientsLock.Unlock()

	// Приветственное сообщение
	welcomeMsg := fmt.Sprintf("Добро пожаловать, %s! Вы в комнате '%s'.\n", nickname, client.room)
	welcomeMsg += co.Commands
	conn.Write([]byte(welcomeMsg))

	// Уведомляем всех о новом участнике
	messages <- fmt.Sprintf("%s присоединился к чату!", nickname)

	remoteAddr := conn.RemoteAddr().(*net.TCPAddr)
	fmt.Printf("Новый клиент: %s (IP: %s)\n", nickname, remoteAddr.IP)

	// Обработка сообщений от клиента
	for {
		conn.Write([]byte("> "))
		msg, err := reader.ReadString('\n')
		if err != nil {
			break
		}

		msg = strings.TrimSpace(msg)

		if msg == "" {
			continue
		}

		// Обработка команд
		if strings.HasPrefix(msg, "/") {
			handleCommand(client, msg)
			continue
		}

		// Отправка обычного сообщения
		timestamp := time.Now().Format("15:04")
		fullMsg := fmt.Sprintf("[%s] %s: %s", timestamp, client.nickname, msg)
		messages <- fullMsg
	}

	// Удаляем клиента при отключении
	clientsLock.Lock()
	delete(clients, client)
	clientsLock.Unlock()

	messages <- fmt.Sprintf("%s покинул чат.", client.nickname)
	fmt.Printf("Клиент отключился: %s\n", client.nickname)
}

func handleCommand(client *Client, cmd string) {
	parts := strings.Fields(cmd)
	if len(parts) == 0 {
		return
	}

	switch parts[0] {
	case "/help":
		client.conn.Write([]byte(co.Commands))

	case "/room":
		if len(parts) < 2 {
			client.conn.Write([]byte("Использование: /room <название_комнаты>\n"))
			return
		}

		oldRoom := client.room
		client.room = parts[1]
		client.conn.Write([]byte(fmt.Sprintf("Вы перешли в комнату: %s\n", client.room)))
		messages <- fmt.Sprintf("%s перешел из '%s' в '%s'", client.nickname, oldRoom, client.room)

	case "/list":
		clientsLock.RLock()
		var users []string
		for c := range clients {
			if c.room == client.room {
				users = append(users, c.nickname)
			}
		}
		clientsLock.RUnlock()

		listMsg := fmt.Sprintf("Участники в комнате '%s' (%d):\n", client.room, len(users))
		for _, user := range users {
			listMsg += fmt.Sprintf("  - %s\n", user)
		}
		client.conn.Write([]byte(listMsg))

	case "/quit":
		client.conn.Write([]byte("До свидания!\n"))
		client.conn.Close()

	default:
		client.conn.Write([]byte("Неизвестная команда. Используйте /help для справки.\n"))
	}
}

func broadcastMessages() {
	for msg := range messages {
		clientsLock.RLock()

		// Для отладки на сервере
		fmt.Printf("Сообщение: %s\n", msg)

		// Отправляем сообщение всем клиентам
		for client := range clients {
			// Можно добавить логику для комнат
			// if client.room == room { ... }

			_, err := client.conn.Write([]byte(msg + "\n"))
			if err != nil {
				// Если ошибка, удаляем клиента
				clientsLock.RUnlock()
				clientsLock.Lock()
				delete(clients, client)
				client.conn.Close()
				clientsLock.Unlock()
				clientsLock.RLock()
			}
		}

		clientsLock.RUnlock()
	}
}
