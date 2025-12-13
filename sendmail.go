package simplemailer

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"net"
	"net/mail"
	"net/smtp"
	"slices"
	"strings"
	"time"
)

// Максимально допустимое время на установку TCP сессии, включая разрешение имени (DNS запрос)
const DIALTIMEOUT_MAXSEC = 10

// Максимально допустимое время на операции чтения записи в/из сокета
const RWTIMEOUT_MAXSEC = 30

type SMTPSettings struct {
	MailFrom         string `xml:"mail_from" json:"mail_from"`
	MailFromName     string `xml:"mail_from_name" json:"mail_from_name"`
	SMTPserver       string `xml:"smtpserver" json:"smtpserver"`
	Username         string `xml:"username" json:"username"`
	SMTPPort         uint   `xml:"smtpport" json:"smtpport"`
	Domain           string `xml:"domain" json:"domain"`
	SMTPPassword     string `xml:"smtp_password" json:"smtp_password"`
	TLSUsage         bool   `xml:"tls" json:"tls"`
	Plain            bool   `xml:"plain" json:"plain"`
	DialTimeoutSec   int    `xml:"connection_timeout_sec" json:"connection_timeout_sec"`
	ConnRWTimeoutSec int    `xml:"smtp_timeout_sec" json:"smtp_timeout_sec"`
	VerifyServerCert bool   `xml:"verify_certificate" json:"verify_certificate"`
}

// Реализация интерфейса механизмов аутентификации - реализуем PLAIN и LOGIN
type loginAuth struct {
	username, password string
}

func LoginAuth(username, password string) smtp.Auth {
	return &loginAuth{username, password}
}

func (a *loginAuth) Start(server *smtp.ServerInfo) (string, []byte, error) {
	//Если нет механизма аутентификации LOGIN и есть только PLAIN, то используем его
	if slices.Contains(server.Auth, "PLAIN") && !slices.Contains(server.Auth, "LOGIN") {
		resp := []byte("\x00" + a.username + "\x00" + a.password)
		return "PLAIN", resp, nil
	}
	return "LOGIN", []byte{}, nil
}

func (a *loginAuth) Next(fromServer []byte, more bool) ([]byte, error) {
	if more {
		switch string(fromServer) {
		case "Username:":
			return []byte(a.username), nil
		case "Password:":
			return []byte(a.password), nil
		default:
			return nil, errors.New("Unknown prompt from server")
		}
	}
	return nil, nil
}

/*---------------------------------------------------------------------------*/

func headerconditioner(headerval string) string {
	headerval = strings.ReplaceAll(headerval, "\r", "")
	headerval = strings.ReplaceAll(headerval, "\n", "")
	headerval = strings.ReplaceAll(headerval, "\t", " ")
	return headerval
}

func validateSMTPserver(host string, port uint) error {
	if host == "" {
		return errors.New("no SMTP host specified")
	}
	if port == 0 {
		return errors.New("SMTP port cannot be 0")
	}
	return nil
}

func SendMail(MailAddr string, MSGSubject string, Message string, settings SMTPSettings, Debuglevel int) (err error) {
	//Таймауты по умолчанию, при использовании этой функции, вы будете уверены в том, что если отправка не удалась,
	//то максимум за время dialtimeout+connrwtimeout (connection_timeout_sec и smtp_timeout_sec в файле settings.json)
	//Она завершиться
	dialtimeout := 5
	connrwtimeout := 5

	if hostvalidation := validateSMTPserver(settings.SMTPserver, settings.SMTPPort); hostvalidation != nil {
		return hostvalidation
	}
	if settings.DialTimeoutSec > 0 && settings.DialTimeoutSec <= DIALTIMEOUT_MAXSEC {
		dialtimeout = settings.DialTimeoutSec
	}
	if settings.ConnRWTimeoutSec > 0 && settings.ConnRWTimeoutSec <= RWTIMEOUT_MAXSEC {
		connrwtimeout = settings.ConnRWTimeoutSec
	}

	tlsusage := settings.TLSUsage

	if _, adderr := mail.ParseAddress(settings.MailFrom); adderr != nil {
		return fmt.Errorf("mail from address error: %v", adderr)
	}

	if _, adderr := mail.ParseAddress(MailAddr); adderr != nil {
		return fmt.Errorf("mail to address error: %v", adderr)
	}

	from := mail.Address{Name: headerconditioner(settings.MailFromName), Address: settings.MailFrom}
	to := mail.Address{Name: "", Address: MailAddr}

	headers := make(map[string]string)
	headers["From"] = from.String()
	headers["To"] = to.String()
	headers["Subject"] = headerconditioner(MSGSubject)
	headers["Content-Type"] = "text/plain; charset=utf-8"

	var mbuilder strings.Builder
	for _, header := range []string{"From", "To", "Subject", "Content-Type"} {
		mbuilder.WriteString(fmt.Sprintf("%s: %s\r\n", header, headers[header]))
	}
	mbuilder.WriteString("\r\n" + Message)
	message := mbuilder.String()
	servername := fmt.Sprintf("%s:%d", settings.SMTPserver, settings.SMTPPort)
	host := settings.SMTPserver
	if Debuglevel > 0 {
		fmt.Println("SMTP server:", servername)
	}

	AuthUsername := settings.Username
	if len(settings.Username) == 0 {
		AuthUsername = settings.MailFrom
	}
	if len(settings.SMTPPassword) == 0 {
		return errors.New("SMTP password is empty")
	}
	authlogin := LoginAuth(AuthUsername, settings.SMTPPassword)
	if Debuglevel > 0 {
		fmt.Println("Login Auth:", authlogin)
	}

	tlsconfig := &tls.Config{
		InsecureSkipVerify: !settings.VerifyServerCert,
		ServerName:         host,
	}

	var conn net.Conn
	var errdial error
	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(dialtimeout)*time.Second)
	defer cancel()

	if tlsusage {
		dialer := &tls.Dialer{Config: tlsconfig}
		conn, errdial = dialer.DialContext(ctx, "tcp", servername)
	} else {
		dialer := &net.Dialer{}
		conn, errdial = dialer.DialContext(ctx, "tcp", servername)
	}
	if errdial != nil {
		return errdial
	}

	var smtpclient *smtp.Client

	err = conn.SetDeadline(time.Now().Add(time.Duration(connrwtimeout) * time.Second))
	if err != nil {
		//необходимо закрыть соединение потому что defer не выполниться
		conn.Close()
		return err
	}

	smtpclient, err = smtp.NewClient(conn, host)
	if err != nil {
		//необходимо закрыть соединение потому что defer не выполниться
		conn.Close()
		return err
	}

	defer func() {
		clerr := smtpclient.Quit()
		if clerr != nil {
			smtpclient.Close()
		}
		if clerr != nil && err == nil {
			err = clerr
		}
	}()

	//Выполним команду HELLO до других команд, таким образом установив имя домена во внутреннюю переменную
	domainname := "powermf"
	if len(settings.Domain) > 0 {
		domainname = settings.Domain
	}
	helloerr := smtpclient.Hello(domainname)
	if helloerr != nil {
		return helloerr
	}

	//Если не указан параметр "Использовать TLS" - tls в json файле, и не указан параметр "работа в открытом виде" - plain в json файле
	//то, используем команду STARTTLS для шифрования дальнейшего SMTP трафика
	if !settings.Plain && !tlsusage {
		starttlserr := smtpclient.StartTLS(tlsconfig)
		if starttlserr != nil {
			return starttlserr
		}
	}

	autherr := smtpclient.Auth(authlogin)
	if autherr != nil {
		return autherr
	}

	fromerr := smtpclient.Mail(from.Address)
	if fromerr != nil {
		return fromerr
	}

	toerr := smtpclient.Rcpt(to.Address)
	if toerr != nil {
		return toerr
	}

	w, dataerr := smtpclient.Data()
	if dataerr != nil {
		return dataerr
	}

	defer func() {
		clerr := w.Close()
		if clerr != nil && err == nil {
			err = clerr
		}
	}()

	_, werr := w.Write([]byte(message))
	if werr != nil {
		return werr
	}
	return err
}
