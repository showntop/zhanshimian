-- 品牌改名：UP一下 → uplook
ALTER TABLE users
  ALTER COLUMN nickname SET DEFAULT 'uplook用户';

UPDATE users
SET nickname = 'uplook用户'
WHERE nickname = 'UP一下用户';
