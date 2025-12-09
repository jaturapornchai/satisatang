# วิธีใช้ ngrok สำหรับ LINE Webhook

## ขั้นตอนการตั้งค่า

### 1. รัน Go Application
```bash
go run .
```
Application จะรันที่ port 3000 (ตามค่าเริ่มต้นใน config)

### 2. รัน ngrok
```bash
ngrok http 3000
```

### 3. คัดลอก Webhook URL
หลังจากรัน ngrok จะได้ URL แบบนี้:
```
https://xxxxxxxx.ngrok-free.app
```

Webhook URL ที่ใช้กับ LINE คือ:
```
https://xxxxxxxx.ngrok-free.app/webhook
```

### 4. ตั้งค่าใน LINE Developers Console
1. ไปที่ [LINE Developers Console](https://developers.line.biz/console/)
2. เลือก Provider และ Channel ของคุณ
3. ไปที่แท็บ **Messaging API**
4. ในส่วน **Webhook settings**:
   - คลิก **Edit** ที่ Webhook URL
   - ใส่ URL: `https://xxxxxxxx.ngrok-free.app/webhook`
   - คลิก **Update**
   - เปิด **Use webhook** (toggle เป็นสีเขียว)
5. คลิก **Verify** เพื่อทดสอบการเชื่อมต่อ

## ตรวจสอบ Requests

### Web Interface
เปิดเบราว์เซอร์ไปที่:
```
http://127.0.0.1:4040
```

ที่นี่คุณจะเห็น:
- Request/Response logs ทั้งหมด
- Request details
- Response status
- Replay requests (สำหรับ debug)

## หมายเหตุ

### URL เปลี่ยนทุกครั้งที่รัน
- ngrok Free plan จะสร้าง URL ใหม่ทุกครั้งที่รัน
- ต้องอัพเดท Webhook URL ใน LINE Console ทุกครั้ง
- ถ้าต้องการ URL คงที่ ต้องใช้ ngrok Paid plan

### ตรวจสอบ Port
- ตรวจสอบว่า Go app รันที่ port อะไรใน `.env`:
  ```
  PORT=3000
  ```
- ใช้ port เดียวกันกับ ngrok:
  ```bash
  ngrok http 3000
  ```

### Troubleshooting

#### Webhook ไม่ทำงาน
1. ตรวจสอบว่า Go app รันอยู่
2. ตรวจสอบว่า ngrok รันอยู่
3. ตรวจสอบ port ว่าตรงกัน
4. ดู logs ที่ ngrok web interface (http://127.0.0.1:4040)
5. ตรวจสอบว่า Webhook URL ใน LINE Console ถูกต้อง

#### 502 Bad Gateway
- Go application ไม่รันหรือ crash
- ตรวจสอบ terminal ที่รัน `go run .`

#### 404 Not Found
- Webhook URL ไม่ถูกต้อง
- ตรวจสอบว่ามี `/webhook` ต่อท้าย URL

## คำสั่งที่ใช้บ่อย

### รัน Application และ ngrok พร้อมกัน
Terminal 1:
```bash
go run .
```

Terminal 2:
```bash
ngrok http 3000
```

### หยุด ngrok
กด `Ctrl+C` ใน terminal ที่รัน ngrok

### ดู ngrok version
```bash
ngrok version
```

### ดู ngrok config
```bash
ngrok config check
```
