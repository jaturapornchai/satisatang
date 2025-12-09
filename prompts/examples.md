# ตัวอย่างคำถาม-คำตอบ

## บันทึกรายการใหม่ (new)

### รายจ่าย
```
ผู้ใช้: กินข้าว 50
ตอบ: {"action":"new","transactions":[{"type":"expense","amount":50,"category":"อาหาร","description":"ข้าว","usetype":0}],"message":"บันทึกค่าอาหาร 50 บาท"}
```

```
ผู้ใช้: เติมน้ำมัน 1500 บัตร KTC
ตอบ: {"action":"new","transactions":[{"type":"expense","amount":1500,"category":"เดินทาง","description":"น้ำมัน","usetype":1,"creditcardname":"KTC"}],"message":"บันทึกค่าน้ำมัน 1,500 บาท (บัตร KTC)"}
```

```
ผู้ใช้: ซื้อของ Lazada 2000 ตัด SCB
ตอบ: {"action":"new","transactions":[{"type":"expense","amount":2000,"category":"ช้อปปิ้ง","description":"Lazada","usetype":2,"bankname":"SCB"}],"message":"บันทึกค่าช้อปปิ้ง 2,000 บาท (SCB)"}
```

### รายรับ
```
ผู้ใช้: เงินเดือน 30000 เข้ากรุงไทย
ตอบ: {"action":"new","transactions":[{"type":"income","amount":30000,"category":"เงินเดือน","description":"เงินเดือน","usetype":2,"bankname":"กรุงไทย"}],"message":"บันทึกเงินเดือน 30,000 บาท (กรุงไทย)"}
```

```
ผู้ใช้: ได้เงินสด 500
ตอบ: {"action":"new","transactions":[{"type":"income","amount":500,"category":"อื่นๆ","description":"ได้เงิน","usetype":0}],"message":"บันทึกรายรับ 500 บาท (เงินสด)"}
```

## ดูยอดคงเหลือ (balance)

```
ผู้ใช้: ยอดคงเหลือ
ตอบ: {"action":"balance","query":{"group_by":"payment"},"message":"ยอดคงเหลือทั้งหมด"}
```

```
ผู้ใช้: ยอด SCB
ตอบ: {"action":"balance","query":{"usetype":2,"bankname":"SCB"},"message":"ยอด SCB"}
```

```
ผู้ใช้: เงินสดเหลือเท่าไหร่
ตอบ: {"action":"balance","query":{"usetype":0},"message":"ยอดเงินสด"}
```

```
ผู้ใช้: หนี้บัตรเครดิต
ตอบ: {"action":"balance","query":{"usetype":1},"message":"ยอดหนี้บัตรเครดิต"}
```

## ค้นหา/วิเคราะห์ (search/analyze)

```
ผู้ใช้: สรุปเดือนนี้
ตอบ: {"action":"analyze","query":{"days":30,"group_by":"category"},"message":"สรุปรายจ่ายเดือนนี้"}
```

```
ผู้ใช้: เคยกินอะไรบ้าง
ตอบ: {"action":"search","query":{"categories":["อาหาร"],"days":30},"message":"รายการอาหาร 30 วันล่าสุด"}
```

```
ผู้ใช้: จ่ายอะไรไปบ้างสัปดาห์นี้
ตอบ: {"action":"search","query":{"type":"expense","days":7},"message":"รายจ่าย 7 วันล่าสุด"}
```

## โอน/ฝาก/ถอน (transfer)

```
ผู้ใช้: โอน 5000 จากกรุงไทยไปกรุงเทพ
ตอบ: {"action":"transfer","transfer":{"from":[{"amount":5000,"usetype":2,"bankname":"กรุงไทย"}],"to":[{"amount":5000,"usetype":2,"bankname":"กรุงเทพ"}],"description":"โอนเงิน"},"message":"โอน 5,000 บาท จากกรุงไทย → กรุงเทพ"}
```

```
ผู้ใช้: ฝากเงิน 10000 เข้ากรุงไทย
ตอบ: {"action":"transfer","transfer":{"from":[{"amount":10000,"usetype":0}],"to":[{"amount":10000,"usetype":2,"bankname":"กรุงไทย"}],"description":"ฝากเงิน"},"message":"ฝากเงิน 10,000 บาท เข้ากรุงไทย"}
```

```
ผู้ใช้: ถอนเงิน 5000 จาก SCB
ตอบ: {"action":"transfer","transfer":{"from":[{"amount":5000,"usetype":2,"bankname":"SCB"}],"to":[{"amount":5000,"usetype":0}],"description":"ถอนเงิน"},"message":"ถอนเงิน 5,000 บาท จาก SCB"}
```

```
ผู้ใช้: จ่ายบัตร KTC 5000 โอนจากกรุงไทย
ตอบ: {"action":"transfer","transfer":{"from":[{"amount":5000,"usetype":2,"bankname":"กรุงไทย"}],"to":[{"amount":5000,"usetype":1,"creditcardname":"KTC"}],"description":"จ่ายบัตรเครดิต"},"message":"จ่ายบัตร KTC 5,000 บาท จากกรุงไทย"}
```

## แก้ไขรายการ (update)

หมายเหตุ: ถ้าผู้ใช้พูดถึงการเปลี่ยนแปลงโดยไม่ระบุจำนวนเงินใหม่ = แก้ไขรายการล่าสุด

```
ผู้ใช้: ไม่ใช่ 50 เป็น 100
ตอบ: {"action":"update","update_field":"amount","update_value":100,"message":"แก้ไขจำนวนเงินเป็น 100 บาท"}
```

```
ผู้ใช้: จ่ายบัตร KTC
ตอบ: {"action":"update","update_field":"usetype","update_value":{"usetype":1,"creditcardname":"KTC"},"message":"แก้ไขเป็นจ่ายบัตร KTC"}
```

```
ผู้ใช้: จ่ายเงินสดนะ
ตอบ: {"action":"update","update_field":"usetype","update_value":{"usetype":0},"message":"แก้ไขเป็นจ่ายเงินสด"}
```

```
ผู้ใช้: เปลี่ยนเป็นตัด SCB
ตอบ: {"action":"update","update_field":"usetype","update_value":{"usetype":2,"bankname":"SCB"},"message":"แก้ไขเป็นตัดจาก SCB"}
```

```
ผู้ใช้: ตัดบัตรนะ ไม่ใช่เงินสด
ตอบ: {"action":"update","update_field":"usetype","update_value":{"usetype":1},"message":"แก้ไขเป็นจ่ายบัตรเครดิต"}
```

## สนทนา (chat)

```
ผู้ใช้: สวัสดี
ตอบ: {"action":"chat","message":"สวัสดีค่ะ! ฉันคือสติสตางค์ ผู้ช่วยจัดการการเงิน บอกได้เลยว่าจะบันทึกอะไรค่ะ"}
```

```
ผู้ใช้: ขอบคุณ
ตอบ: {"action":"chat","message":"ยินดีค่ะ! มีอะไรให้ช่วยอีกไหมคะ"}
```
