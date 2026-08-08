package mcpserver

const dataListOutputSchemaJSON = `{
  "type": "object",
  "description": "利用可能な市場データprovider、dataset、入力項目の一覧です。",
  "properties": {
    "version": {
      "type": "string",
      "description": "API契約のバージョンです。"
    },
    "providers": {
      "type": "array",
      "description": "現在の設定で利用できるproviderの一覧です。",
      "items": {
        "type": "object",
        "properties": {
          "name": {
            "type": "string",
            "description": "collectのproviderへ指定する安定識別子です。"
          },
          "display_name": {
            "type": "string",
            "description": "providerの表示名です。"
          },
          "description": {
            "type": "string",
            "description": "providerの概要です。"
          },
          "datasets": {
            "type": "array",
            "description": "providerから収集できるdatasetの一覧です。",
            "items": {
              "type": "object",
              "properties": {
                "name": {
                  "type": "string",
                  "description": "collectのdatasetへ指定する安定識別子です。"
                },
                "description": {
                  "type": "string",
                  "description": "datasetの概要です。"
                },
                "parameters": {
                  "type": "array",
                  "description": "collectのparametersへ指定できる入力項目です。",
                  "items": {
                    "type": "object",
                    "properties": {
                      "name": {
                        "type": "string",
                        "description": "入力項目名です。"
                      },
                      "type": {
                        "type": "string",
                        "description": "入力値の型です。"
                      },
                      "required": {
                        "type": "boolean",
                        "description": "入力が必須かを表します。"
                      },
                      "description": {
                        "type": "string",
                        "description": "入力項目の説明です。"
                      },
                      "allowed": {
                        "type": "array",
                        "description": "指定可能な値が限定される場合の候補です。",
                        "items": {
                          "type": "string"
                        }
                      },
                      "default": {
                        "description": "省略時の既定値です。"
                      }
                    },
                    "required": ["name", "type", "required", "description"],
                    "additionalProperties": false
                  }
                }
              },
              "required": ["name", "description", "parameters"],
              "additionalProperties": false
            }
          }
        },
        "required": ["name", "display_name", "description", "datasets"],
        "additionalProperties": false
      }
    }
  },
  "required": ["version", "providers"],
  "additionalProperties": false
}`

const collectOutputSchemaJSON = `{
  "type": "object",
  "description": "指定したproviderから収集した市場データです。",
  "properties": {
    "version": {
      "type": "string",
      "description": "API契約のバージョンです。"
    },
    "provider": {
      "type": "string",
      "description": "収集に使用したproviderの識別子です。"
    },
    "dataset": {
      "type": "string",
      "description": "収集したdatasetの識別子です。"
    },
    "collected_at": {
      "type": "string",
      "format": "date-time",
      "description": "provider処理が完了したUTC日時です。"
    },
    "metadata": {
      "type": "object",
      "description": "取得元やページングなど、結果の解釈に必要な付帯情報です。",
      "additionalProperties": true
    },
    "data": {
      "description": "providerとdatasetに固有の市場データです。具体的な形はdatalistと各provider仕様で確認します。"
    }
  },
  "required": ["version", "provider", "dataset", "collected_at", "data"],
  "additionalProperties": false
}`
