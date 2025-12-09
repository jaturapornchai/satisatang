# โครงสร้างข้อมูล สติสตางค์

## หลักการคำนวณ
- **ยอดคงเหลือ** = sum(amount × type) โดย type=1 (รายรับ), type=-1 (รายจ่าย)
- **ทรัพย์สินสุทธิ** = เงินสด + ธนาคารทั้งหมด - หนี้บัตรเครดิต

## ประเภทการจ่าย (usetype)
- `0` = เงินสด
- `1` = บัตรเครดิต (หนี้สิน)
- `2` = ธนาคาร

## ประเภทธุรกรรม (type)
- `1` = รายรับ (income) - เงินเข้า
- `-1` = รายจ่าย (expense) - เงินออก

---

## Collection: daily_records
```json
{
    "_id": "ObjectId",
    "lineid": "U123456789",
    "date": "2024-05-14",
    "transactions": [
        {
            "_id": "ObjectId",
            "type": 1,
            "amount": 30000,
            "category": "เงินเดือน",
            "description": "เงินเดือน พ.ค.",
            "custname": "บริษัท ABC",
            "usetype": 2,
            "bankname": "กรุงไทย",
            "creditcardname": "",
            "transfer_id": "",
            "imagebase64": "",
            "created_at": "2024-05-14T09:00:00Z"
        },
        {
            "_id": "ObjectId",
            "type": -1,
            "amount": 150,
            "category": "อาหาร",
            "description": "มื้อกลางวัน",
            "custname": "ร้านข้าวแกง",
            "usetype": 0,
            "bankname": "",
            "creditcardname": "",
            "transfer_id": "",
            "imagebase64": "",
            "created_at": "2024-05-14T12:30:00Z"
        }
    ],
    "created_at": "2024-05-14T00:00:00Z",
    "updated_at": "2024-05-14T12:30:00Z"
}
```

---

## Collection: transfers (การโอน/ฝาก/ถอน)
```json
{
    "_id": "ObjectId",
    "lineid": "U123456789",
    "date": "2024-05-14",
    "description": "โอนเงินจากกรุงไทยไปกรุงเทพ",
    "from": [
        {
            "amount": 5000,
            "usetype": 2,
            "bankname": "กรุงไทย",
            "creditcardname": ""
        }
    ],
    "to": [
        {
            "amount": 5000,
            "usetype": 2,
            "bankname": "กรุงเทพ",
            "creditcardname": ""
        }
    ],
    "total_amount": 5000,
    "created_at": "2024-05-14T10:00:00Z"
}
```

---

## ตัวอย่างการใช้งานในชีวิตประจำวัน

### 1. รายรับ (Income)
```
"เงินเดือน 30000 เข้ากรุงไทย"
→ type=1, amount=30000, usetype=2, bankname="กรุงไทย"

"ได้รับเงินสด 500"
→ type=1, amount=500, usetype=0

"โบนัส 10000 เข้า SCB"
→ type=1, amount=10000, usetype=2, bankname="SCB"
```

### 2. รายจ่าย (Expense)
```
"กินข้าว 150 บาท"
→ type=-1, amount=150, usetype=0 (default เงินสด)

"จ่ายค่าน้ำมัน 1500 บัตร KTC"
→ type=-1, amount=1500, usetype=1, creditcardname="KTC"

"ซื้อของออนไลน์ 2000 ตัดจากกสิกร"
→ type=-1, amount=2000, usetype=2, bankname="กสิกร"
```

### 3. การโอนเงินระหว่างบัญชี (Transfer)
```
"โอน 5000 จากกรุงไทยไปกรุงเทพ"
→ transfer: from=[{5000,usetype=2,bankname="กรุงไทย"}] to=[{5000,usetype=2,bankname="กรุงเทพ"}]
→ สร้าง 2 transactions:
   - type=-1, amount=5000, usetype=2, bankname="กรุงไทย" (เงินออก)
   - type=1, amount=5000, usetype=2, bankname="กรุงเทพ" (เงินเข้า)
```

### 4. ฝากเงิน (Deposit)
```
"ฝากเงิน 10000 เข้ากรุงไทย"
→ transfer: from=[{10000,usetype=0}] to=[{10000,usetype=2,bankname="กรุงไทย"}]
→ สร้าง 2 transactions:
   - type=-1, amount=10000, usetype=0 (เงินสดออก)
   - type=1, amount=10000, usetype=2, bankname="กรุงไทย" (เข้าธนาคาร)
```

### 5. ถอนเงิน (Withdraw)
```
"ถอนเงิน 5000 จากกสิกร"
→ transfer: from=[{5000,usetype=2,bankname="กสิกร"}] to=[{5000,usetype=0}]
→ สร้าง 2 transactions:
   - type=-1, amount=5000, usetype=2, bankname="กสิกร" (ออกจากธนาคาร)
   - type=1, amount=5000, usetype=0 (เข้าเงินสด)
```

### 6. จ่ายบัตรเครดิต (Pay Credit Card)
```
"จ่ายบัตร KTC 5000 โอนจากกรุงไทย"
→ transfer: from=[{5000,usetype=2,bankname="กรุงไทย"}] to=[{5000,usetype=1,creditcardname="KTC"}]
→ สร้าง 2 transactions:
   - type=-1, amount=5000, usetype=2, bankname="กรุงไทย" (ออกจากธนาคาร)
   - type=1, amount=5000, usetype=1, creditcardname="KTC" (ลดหนี้บัตร)
```

### 7. Many-to-Many Transfer
```
"โอน 3000 จากเงินสด และ 2000 จากกรุงไทย รวมเข้ากรุงเทพ"
→ transfer:
   from=[{3000,usetype=0}, {2000,usetype=2,bankname="กรุงไทย"}]
   to=[{5000,usetype=2,bankname="กรุงเทพ"}]
→ สร้าง 3 transactions:
   - type=-1, amount=3000, usetype=0 (เงินสดออก)
   - type=-1, amount=2000, usetype=2, bankname="กรุงไทย" (ออกจากกรุงไทย)
   - type=1, amount=5000, usetype=2, bankname="กรุงเทพ" (เข้ากรุงเทพ)
```

---

## Collection: chat_history
```json
{
    "_id": "ObjectId",
    "lineid": "U123456789",
    "messages": [
        {
            "role": "user",
            "content": "กินข้าว 150",
            "timestamp": "2024-05-14T12:30:00Z"
        },
        {
            "role": "assistant",
            "content": "บันทึกรายจ่าย 150 บาท หมวดอาหาร",
            "timestamp": "2024-05-14T12:30:01Z"
        }
    ],
    "updated_at": "2024-05-14T12:30:01Z"
}
```

---

## AI Response Actions

| Action | คำอธิบาย | ตัวอย่าง |
|--------|---------|---------|
| `new` | บันทึกรายการใหม่ | "กินข้าว 150" |
| `transfer` | โอน/ฝาก/ถอน | "โอน 1000 จากกรุงไทยไปกรุงเทพ" |
| `update` | แก้ไขรายการล่าสุด | "ไม่ใช่ 150 เป็น 200" |
| `balance` | ดูยอดคงเหลือ | "ยอดเหลือเท่าไหร่" |
| `chat` | สนทนาทั่วไป | "สวัสดี" |

---

## หมายเหตุ
- `transfer_id` ใน transaction ใช้เชื่อมกับ transfers collection (ถ้าเป็นการโอน)
- ทุก transaction มี `usetype`, `bankname`, `creditcardname` ระบุที่มา/ปลายทางของเงิน
- การคำนวณยอดคงเหลือแต่ละประเภท: `sum(amount × type)` group by usetype + bankname/creditcardname
