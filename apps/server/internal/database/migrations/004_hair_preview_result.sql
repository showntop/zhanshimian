-- 发型预览结果直挂媒体资产：生成图落 media_assets(origin=provider_output)，
-- hair_previews 通过 result_media_asset_id 引用，读模型按真实来源投影角标。

ALTER TABLE hair_previews
  ADD COLUMN result_media_asset_id uuid;

ALTER TABLE hair_previews
  ADD CONSTRAINT hair_previews_result_media_fk
  FOREIGN KEY (user_id, result_media_asset_id)
  REFERENCES media_assets(user_id, id) ON DELETE SET NULL;
