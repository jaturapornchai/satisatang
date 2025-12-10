ตัวอย่างการตอบ (สมมติ สรุปยอด|ยอดรวม:50000|กสิกร:30000|เงินสด:20000):

ผู้ใช้: กินข้าว 50
{"action":"new","transactions":[{"amount":50,"type":"expense","category":"อาหาร","description":"กินข้าว","usetype":0}],"message":"บันทึกค่าอาหาร 50 บาท คงเหลือ 49,950 บาทค่ะ"}

ผู้ใช้: เงินเดือน 30000 เข้ากสิกร
{"action":"new","transactions":[{"amount":30000,"type":"income","category":"เงินเดือน","description":"เงินเดือน","usetype":2,"bankname":"กสิกร"}],"message":"บันทึกเงินเดือน 30,000 บาท กสิกรมี 60,000 บาท รวม 80,000 บาทค่ะ"}

ผู้ใช้: กาแฟ 65 บัตร KTC
{"action":"new","transactions":[{"amount":65,"type":"expense","category":"เครื่องดื่ม","description":"กาแฟ","usetype":1,"creditcardname":"KTC"}],"message":"บันทึกค่ากาแฟ 65 บาท (บัตร KTC) คงเหลือ 49,935 บาทค่ะ"}

ผู้ใช้: โอน 1000 จากกสิกรไป SCB
{"action":"transfer","transfer":{"from":[{"amount":1000,"usetype":2,"bankname":"กสิกร"}],"to":[{"amount":1000,"usetype":2,"bankname":"ไทยพาณิชย์"}]},"message":"โอน 1,000 บาท กสิกร→ไทยพาณิชย์ รวม 50,000 บาทค่ะ"}

ผู้ใช้: ฝากเงิน 5000 เข้ากรุงไทย
{"action":"transfer","transfer":{"from":[{"amount":5000,"usetype":0}],"to":[{"amount":5000,"usetype":2,"bankname":"กรุงไทย"}]},"message":"ฝาก 5,000 บาทเข้ากรุงไทย เงินสดเหลือ 15,000 บาทค่ะ"}

ผู้ใช้: ถอนเงิน 2000 จาก SCB
{"action":"transfer","transfer":{"from":[{"amount":2000,"usetype":2,"bankname":"ไทยพาณิชย์"}],"to":[{"amount":2000,"usetype":0}]},"message":"ถอน 2,000 บาทจากไทยพาณิชย์ เงินสดมี 22,000 บาทค่ะ"}

ผู้ใช้: ยอดคงเหลือ
{"action":"balance","query":{"type":"all"},"message":"ยอดรวม 50,000 บาท (กสิกร 30,000 + เงินสด 20,000)"}

ผู้ใช้: สรุป 7 วัน
{"action":"analyze","query":{"type":"all","days":7,"group_by":"category"},"message":"7 วัน รายรับ 35,000 รายจ่าย 5,000 คงเหลือ 50,000 บาทค่ะ"}

ผู้ใช้: หาค่ากาแฟ
{"action":"search","query":{"keyword":"กาแฟ","days":30},"message":"พบรายการกาแฟ ยอดรวม 50,000 บาทค่ะ"}

ผู้ใช้: ตั้งงบอาหาร 5000
{"action":"budget","budget":{"category":"อาหาร","amount":5000},"message":"ตั้งงบอาหาร 5,000/เดือน ยอดคงเหลือ 50,000 บาทค่ะ"}

ผู้ใช้: export excel
{"action":"export","export":{"format":"excel","days":30},"message":"สร้าง Excel 30 วัน ยอดรวม 50,000 บาทค่ะ"}

ผู้ใช้: สวัสดี
{"action":"chat","message":"สวัสดีค่ะ ยอดคงเหลือ 50,000 บาท มีอะไรให้ช่วยคะ?"}
