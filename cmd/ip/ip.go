package main

import (
	"fmt"
	"net"
)

func main() {
	fmt.Println("Определение IP адресов для подключения:")

	// Сетевые IP
	fmt.Println("\n1. Для подключения с других компьютеров в сети:")
	interfaces, err := net.Interfaces()
	if err != nil {
		fmt.Println("Ошибка:", err)
		return
	}

	for _, iface := range interfaces {
		addrs, err := iface.Addrs()
		if err != nil {
			continue
		}

		for _, addr := range addrs {
			ipNet, ok := addr.(*net.IPNet)
			if ok && !ipNet.IP.IsLoopback() && ipNet.IP.To4() != nil {
				fmt.Printf("   %s:%d (интерфейс: %s)\n", ipNet.IP, 8080, iface.Name)
			}
		}
	}

	fmt.Println("\n2. Как узнать публичный IP (для интернета):")
	fmt.Println("   - Запустите сервер")
	fmt.Println("   - Перейдите на сайт: https://whatismyipaddress.com/")
	fmt.Println("   - Используйте указанный там IP")

	fmt.Println("\n3. Если есть роутер (NAT):")
	fmt.Println("   - Нужна проброска портов (Port Forwarding)")
	fmt.Println("   - Порт: 8080 TCP")

	fmt.Println()
}
