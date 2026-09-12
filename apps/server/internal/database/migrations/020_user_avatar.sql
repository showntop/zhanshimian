-- 账户展示头像，与建档三张照片分开。删除媒体时头像引用清空。
ALTER TABLE users ADD COLUMN avatar_media_id uuid REFERENCES media_assets(id) ON DELETE SET NULL;
