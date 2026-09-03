-- 反馈可选附带上身实拍：记录用户自己上传的 feedback 照片，便于后续顾问判断
ALTER TABLE feedback ADD COLUMN media_id uuid REFERENCES media_assets(id) ON DELETE SET NULL;
