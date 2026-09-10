-- 品牌改名：怎么打扮 → UP一下
ALTER TABLE users
  ALTER COLUMN nickname SET DEFAULT 'UP一下用户';

UPDATE users
SET nickname = 'UP一下用户'
WHERE nickname = '怎么打扮用户';
