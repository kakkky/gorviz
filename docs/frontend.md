# Frontend

gorviz-server の GUI を、templ・hotwire-go・Stimulus・Preact (turbo-mount) で作るときの使い分けと置き場。振る舞いの仕様は [design.md](design.md)、コード全体の構成は [architecture.md](architecture.md) にある。

## 前提

- HTML は server が templ で描く。画面の行き来とライブ更新は Turbo (Drive・Frames・SSE の Streams) に任せる
- client で描画まで持つのは、server の描く HTML では表せないものだけにする
- 判定・診断・集計は server の `domain` に置く。client には表示のための計算だけを置く

## 使い分け

上から順に検討し、そこで済むなら下へ降りない。

1. JS を書かない
   - リンク・フォーム・`turbo-frame`・SSE の `turbo-stream` で済むとき
2. Stimulus
   - server の描いた DOM に振る舞いを足すとき
   - 状態はほとんど持たない
   - DOM を組み立てない。出すものは server が描いておき、JS は表示を切り替えるだけにする
   - DOM を自分で操作するライブラリを使うときも、Stimulus の `connect` で作り `disconnect` で破棄する
3. Preact のアイランド
   - client で状態を持ち、その状態から何度も描き直すとき
   - 目安は「server に聞かずに描き直す回数が多い」か「JS で DOM を組み立て始めた」か
   - [turbo-mount](https://github.com/skryukov/turbo-mount) で載せる

## View (`presentation/view/`)

- `layout/`
  - 全画面を包む外枠
- `pages/`
  - 画面 1 つに 1 つ
- `components/`
  - 画面をまたいで使う部品
- `partials/`
  - Turbo Frame・Turbo Stream で一部だけを返す断片

テンプレートは `<名前>ViewModel` の構造体を 1 つ受け取る。handler が usecase の出力から組み立て、テンプレートには表示の分岐だけを書く。

## Stimulus

- controller は `assets/src/controllers/<名前>_controller.ts` に書く
- Go 側では `assets/controllers.go` に `Controller{Name, Path}` として宣言し、テンプレートからはこれで参照する。controller を足すときは、TS とこの宣言をセットで足す
- 属性は hotwire-go の `stimulus.AttrControllers`・`AttrTargets`・`AttrValue`・`AttrActions` を spread して書く
- import map は Turbo で画面を移ったあとには足せないので、`layout.Base` の `stimulus.ScriptLoad` でどの画面でもすべての controller を読み込む

## Preact のアイランド

アイランド ([Islands Architecture](https://www.patterns.dev/vanilla/islands-architecture/)) は、server の描いたページの中で、Preact が描画と状態を受け持つ 1 つの要素。アイランドの外は server の HTML と Turbo・Stimulus が受け持つ。

### 載せ方

- server は、turbo-mount の形の要素を描く
  ```html
  <div data-controller="turbo-mount-<名前>"
       data-turbo-mount-<名前>-component-value="<名前>"
       data-turbo-mount-<名前>-props-value="{...}"></div>
  ```
  - props は Go の構造体を JSON にして渡す
- client は、turbo-mount に Preact のプラグインを足して使う
  - プラグインは `render(h(Component, props), el)` で載せ、`render(null, el)` で外す関数を返すだけにする
- アイランドの登録は `assets/src/islands.ts` にまとめ、`stimulus.ScriptLoad` の後に module として読む。turbo-mount は `window.Stimulus` を使うので、Stimulus は 1 つだけにする

### Turbo との境界

- アイランドの要素の中は Preact だけが触る
  - SSE の `turbo-stream` の宛先や、morph で書き換わる要素は、アイランドの要素の外に置く。アイランドの要素そのものの morph は turbo-mount が props の属性だけに留める
  - アイランドの中で Stimulus の `data-action` や `turbo-frame` を使わない。イベントは Preact の `onClick` などで受ける
- Turbo のページキャッシュには、`turbo:before-cache` でアイランドを外した空の要素を写させる。戻ったときに古い DOM の上から描かないため
- アイランドの外 (URL・`turbo-frame` の src・ほかの要素) を変えるのは、アイランドの controller だけにする
  - Preact からは `document` を触らない。アイランドの外に何かしたいときは、コールバックで controller に伝える
  - アイランドの中の普通の `<a href>` は、そのまま Turbo Drive の遷移になる

### 届き続けるデータ

- props で渡すのは初期値だけにする。props が変わると turbo-mount はアイランドを作り直し、アイランドの中の状態が消える
- SSE で届き続けるものは、`TurboMountController` を継承したアイランドの controller が受け、signal でアイランドに流し込む

### アイランドの中の分け方

- `assets/src/<アイランド>/` に 1 つのアイランドを置く
- DOM を触らない計算は純粋な TS にし、単体テストを書く
- 描画は役割ごとのコンポーネントに分け、各コンポーネントは自分の描画に要る状態と props だけを持つ

## server との約束

- 初期データは props (または `<script type="application/json">`) で HTML に埋め、更新は SSE の `turbo-stream` で届ける
- client に渡す JSON の型と、server と client で揃えるコード値は Go から TS を生成する。手で写さない
- 同じ表示は片方だけが持つ。server が描く部分は templ、アイランドの中は TS が持ち、両方に同じ関数を置かない

## ビルド

- client のコードはすべて TS で `assets/src/` に書き、esbuild で `assets/dist/js/` に書き出す
- 依存 (esbuild・TypeScript・preact・turbo-mount) は `assets/package.json` の `devDependencies` に置く。npm を使うのは開発のときだけ
- `@hotwired/stimulus` はバンドルに含めず、hotwire-go の import map から読む
- `assets/dist/` はコミットする。`go build` と配布に Node は要らない
- 型は `tsc --noEmit` で確かめる

## 静的ファイルの URL

- `assets/dist/` のファイルは `assets.Versioned` を通して参照する。中身から作った版を `?v=` に付ける
- Turbo は画面を移っても同じ URL の CSS と JS を読み直さないので、作り直したら URL が変わるようにしている。`data-turbo-track="reload"` で、版が変わったら画面ごと読み直す
