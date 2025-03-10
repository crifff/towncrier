# towncrier
タウンクライヤーはDiscordで1つのボイスチャンネルから複数のボイスチャンネルに音声をブロードキャストするためのツールです。

## 準備
タウンクライヤーは3つ以上のDiscordボットを必要とします。同一のサーバーにインストールされ、ボイスチャンネルに参加する権限を有しておく必要があります。
1. タウンクライヤー本体
2. 発信元となる親機
3. 送信先となる子機（複数台可）

それぞれのBOTのトークンを`tokens.txt`に書いて実行ファイルと同じ階層に配置してください。１行目が本体、２行目が親機、３行目以降は子機として取り扱われます。
```text:tokens.txt
MTkdf8YYlkerlkJe... #本体のトークン
MT939ds5dSfdmlMe... #親機のトークン
MTzbdBC875NAwk0x... #子機01のトークン
MT076nNGIO2112Gf... #子機02のトークン
...
```

## 実行
実行ファイルとtokens.txtを配置した場所でコマンドを実行します。
引数に実行するDiscordサーバーの招待リンクを渡してください。

```shell
  towncrier.exe https://discord.gg/xXxXxXxXx
```

実行するとBOTがすべてオンラインになり、コマンドが利用可能になります。

## 使い方
Discord上のコマンドでBOTを操作してください。
チャット欄にスラッシュを入力するとコマンドが表示されるのでタウンクライヤーのコマンドを選択してください。
親機と子機を異なるボイスチャンネルに入室させると、親機から子機へ音声が流れます。子機の数が増えるほど転送量が増えるため上りの帯域に注意してください。

### Discord上のコマンド
#### /親機を入室させる <channel>
指定したチャンネルに親機を入室させます。親機は１台のみで、親機が入室済みのときに他のチャンネルへの入室を命令すると移動します。

#### /親機を退室させる
親機がチャンネルから切断し、音声が子機へ配信されなくなります。

#### /子機を入室させる <channel>
指定したチャンネルに子機を入室させます。複数の子機BOTを登録している場合空いている子機から適当に入室します。全ての子機が入室している状態で新たに子機を入室させようとするとエラーになります。

#### /子機を退室させる <channel>
子機がチャンネルから切断されます。

### /親機と全ての子機を退室させる
親機と子機がすべてチャンネルから切断されます。


## 終了
Ctrl+Cを押して終了してください。