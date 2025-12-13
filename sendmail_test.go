package simplemailer

import (
	"encoding/json"
	"os"
	"testing"
)

const (
	settingsfile = "settingsprivate.json"
	mailto       = "oleg.powerc@gmail.com"
	subject      = "тестовое письмо"
	bsgbody      = "тело письма"
)

func TestSendMail(t *testing.T) {
	//Загрузим данные из файла settings.json
	/*
		exfullpath, err := os.Executable()
		if err != nil {
			t.Error(err)
		}
		exPath := filepath.Dir(exfullpath)
		PathSeparator := "/"
		if runtime.GOOS == "windows" {
			PathSeparator = "\\"
		}

		SettingsFileFullName := fmt.Sprintf("%s%s%s", exPath, PathSeparator, settingsfile)
	*/
	SettingsFileFullName := settingsfile
	SettingsRead, SettingsReadErr := os.ReadFile(SettingsFileFullName)
	if SettingsReadErr != nil {
		t.Error(SettingsReadErr)
	}

	var msettings SMTPSettings
	JsonUMErr := json.Unmarshal(SettingsRead, &msettings)
	if JsonUMErr != nil {
		t.Error(JsonUMErr)
	}

	SErr := SendMail(mailto, subject, bsgbody, msettings, 0)
	if SErr != nil {
		t.Error(SErr)
	}

}
