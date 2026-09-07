ALTER TABLE users
  ALTER COLUMN nickname SET DEFAULT '怎么打扮用户';

UPDATE users
SET nickname = '怎么打扮用户'
WHERE nickname = '见我用户';
