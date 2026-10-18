; ============================================================================
; Show Me The Story — Windows 安装包（简体中文向导）
;
; 构建：makensis /DAPP_VERSION=4.0.2 /DSOURCE_EXE=... installer.nsi
; 产物：ShowMeTheStory-Setup-<version>-windows-amd64.exe
;
; 设计要点
;  - 每用户安装（无需管理员），装到 %LOCALAPPDATA%\Programs\ShowMeTheStory
;  - 中文向导；系统不是中文时回退英文
;  - 保留用户数据（storys/api.json/skills）不随卸载删除
;  - 数据目录固定为安装目录，避免双击时落到 System32
; ============================================================================

Unicode true
SetCompressor /SOLID lzma
SetCompressorDictSize 64

!include "MUI2.nsh"
!include "FileFunc.nsh"
!include "LogicLib.nsh"

; ---------- 可被命令行覆盖的参数 ----------
!ifndef APP_VERSION
  !define APP_VERSION "4.0.2"
!endif
; NSIS 的 VIProductVersion 只接受 X.X.X.X（纯数字四段），
; 而 APP_VERSION 可能带后缀（如 4.0.2-ui），所以单独传一个数字版本。
!ifndef APP_VERSION_NUMERIC
  !define APP_VERSION_NUMERIC "4.0.2.0"
!endif
!ifndef SOURCE_EXE
  !define SOURCE_EXE "..\dist\show-me-the-story.exe"
!endif
!ifndef OUTPUT_DIR
  !define OUTPUT_DIR "..\dist"
!endif

!define APP_NAME        "Show Me The Story"
!define APP_NAME_ZH     "AI 小说写手"
!define APP_PUBLISHER   "xianii"
!define APP_URL         "https://github.com/ShiYao3286/show-me-the-story"
!define APP_EXE         "show-me-the-story.exe"
!define APP_ID          "ShowMeTheStory"

!define INSTALL_KEY     "Software\Microsoft\Windows\CurrentVersion\Uninstall\${APP_ID}"
!define REG_APP         "Software\${APP_ID}"

; 每用户安装目录（无 UAC）
InstallDir "$LOCALAPPDATA\Programs\${APP_ID}"
InstallDirRegKey HKCU "${REG_APP}" "InstallDir"
RequestExecutionLevel user
BrandingText "${APP_NAME} ${APP_VERSION}"

Name "${APP_NAME_ZH} (${APP_NAME}) ${APP_VERSION}"
OutFile "${OUTPUT_DIR}\ShowMeTheStory-Setup-${APP_VERSION}-windows-amd64.exe"

VIProductVersion "${APP_VERSION_NUMERIC}"
VIAddVersionKey "ProductName"     "${APP_NAME}"
VIAddVersionKey "ProductVersion"  "${APP_VERSION}"
VIAddVersionKey "FileDescription" "${APP_NAME_ZH} 安装程序"
VIAddVersionKey "FileVersion"     "${APP_VERSION}"
VIAddVersionKey "CompanyName"     "${APP_PUBLISHER}"
VIAddVersionKey "LegalCopyright"  "MIT License"

; ---------- 界面 ----------
!define MUI_ICON   "icon.ico"
!define MUI_UNICON "icon.ico"
!define MUI_ABORTWARNING
!define MUI_WELCOMEPAGE_TITLE "欢迎安装 ${APP_NAME_ZH}"
!define MUI_WELCOMEPAGE_TEXT  "这是一个本地运行的 AI 长篇小说创作工具。$\r$\n$\r$\n安装完成后，从开始菜单或桌面快捷方式启动，程序会在浏览器中打开写作界面（默认地址 http://localhost:48090）。$\r$\n$\r$\n你的小说数据保存在安装目录下的 storys 文件夹中；卸载时不会删除这些数据。"
!define MUI_FINISHPAGE_RUN "$INSTDIR\${APP_EXE}"
!define MUI_FINISHPAGE_RUN_TEXT "立即启动 ${APP_NAME_ZH}"
!define MUI_FINISHPAGE_SHOWREADME ""
!define MUI_FINISHPAGE_SHOWREADME_NOTCHECKED

!insertmacro MUI_PAGE_WELCOME
!insertmacro MUI_PAGE_DIRECTORY
!insertmacro MUI_PAGE_INSTFILES
!insertmacro MUI_PAGE_FINISH

!insertmacro MUI_UNPAGE_CONFIRM
!insertmacro MUI_UNPAGE_INSTFILES

; 简体中文优先，英文兜底
!insertmacro MUI_LANGUAGE "SimpChinese"
!insertmacro MUI_LANGUAGE "English"

; ---------- 安装 ----------
Function .onInit
  ; 检测是否已安装，提示升级
  ReadRegStr $0 HKCU "${REG_APP}" "InstallDir"
  ${If} $0 != ""
    ReadRegStr $1 HKCU "${INSTALL_KEY}" "DisplayVersion"
    MessageBox MB_OKCANCEL|MB_ICONINFORMATION \
      "检测到已安装 ${APP_NAME_ZH} ${APP_VERSION}（旧版本：$1）。$\r$\n$\r$\n点击「确定」继续安装/升级，小说数据会保留。" \
      IDOK +2
    Abort
  ${EndIf}
FunctionEnd

Section "主程序" SecMain
  SectionIn RO

  SetOutPath "$INSTDIR"
  ; 保留用户数据：只覆盖程序文件
  File "${SOURCE_EXE}"

  ; 数据目录与配置放在安装目录（程序默认使用工作目录）
  CreateDirectory "$INSTDIR\storys"

  ; 写注册表
  WriteRegStr HKCU "${REG_APP}" "InstallDir" "$INSTDIR"
  WriteRegStr HKCU "${REG_APP}" "Version" "${APP_VERSION}"

  ; 卸载信息（控制面板“应用和功能”）
  WriteRegStr   HKCU "${INSTALL_KEY}" "DisplayName"     "${APP_NAME_ZH} (${APP_NAME})"
  WriteRegStr   HKCU "${INSTALL_KEY}" "DisplayVersion"  "${APP_VERSION}"
  WriteRegStr   HKCU "${INSTALL_KEY}" "Publisher"       "${APP_PUBLISHER}"
  WriteRegStr   HKCU "${INSTALL_KEY}" "URLInfoAbout"    "${APP_URL}"
  WriteRegStr   HKCU "${INSTALL_KEY}" "DisplayIcon"     "$INSTDIR\${APP_EXE}"
  WriteRegStr   HKCU "${INSTALL_KEY}" "InstallLocation" "$INSTDIR"
  WriteRegStr   HKCU "${INSTALL_KEY}" "UninstallString" '"$INSTDIR\uninstall.exe"'
  WriteRegDWORD HKCU "${INSTALL_KEY}" "NoModify" 1
  WriteRegDWORD HKCU "${INSTALL_KEY}" "NoRepair" 1

  WriteUninstaller "$INSTDIR\uninstall.exe"

  ; 开始菜单
  CreateDirectory "$SMPROGRAMS\${APP_NAME}"
  CreateShortcut  "$SMPROGRAMS\${APP_NAME}\${APP_NAME_ZH}.lnk" "$INSTDIR\${APP_EXE}" "" "$INSTDIR\${APP_EXE}" 0
  CreateShortcut  "$SMPROGRAMS\${APP_NAME}\卸载 ${APP_NAME_ZH}.lnk" "$INSTDIR\uninstall.exe"

  ; 桌面快捷方式（可选组件）
  ${GetParameters} $R0
SectionEnd

Section "桌面快捷方式" SecDesktop
  CreateShortcut "$DESKTOP\${APP_NAME_ZH}.lnk" "$INSTDIR\${APP_EXE}" "" "$INSTDIR\${APP_EXE}" 0
SectionEnd

; 卸载时可选保留数据
Section "Uninstall"
  ; 删除程序文件（保留 storys / api.json / skills 用户数据）
  Delete "$INSTDIR\${APP_EXE}"
  Delete "$INSTDIR\uninstall.exe"

  Delete "$SMPROGRAMS\${APP_NAME}\${APP_NAME_ZH}.lnk"
  Delete "$SMPROGRAMS\${APP_NAME}\卸载 ${APP_NAME_ZH}.lnk"
  RMDir  "$SMPROGRAMS\${APP_NAME}"
  Delete "$DESKTOP\${APP_NAME_ZH}.lnk"

  ; 询问是否删除数据
  IfFileExists "$INSTDIR\storys\*.*" 0 noData
    MessageBox MB_YESNO|MB_ICONQUESTION \
      "是否同时删除小说数据？$\r$\n$\r$\n选择「否」将保留 storys、api.json、skills 等文件，便于以后重新安装继续使用。" \
      IDYES deleteData IDNO noData
  deleteData:
    RMDir /r "$INSTDIR\storys"
    Delete "$INSTDIR\api.json"
    RMDir /r "$INSTDIR\skills"
    Delete "$INSTDIR\dev.log"
  noData:

  RMDir "$INSTDIR"

  DeleteRegKey HKCU "${INSTALL_KEY}"
  DeleteRegKey HKCU "${REG_APP}"
SectionEnd
