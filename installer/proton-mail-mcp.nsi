; Windows installer for Proton Mail MCP (NSIS 3, Unicode). Built by
; `./tools/task.ps1 installer`, which passes:
;   VERSION      e.g. 0.2.1
;   FILEVERSION  e.g. 0.2.1.0
;   ARCH         x64 or arm64
;   SRCDIR       the folder with proton-mail-mcp.exe, proton-mail-mcp-setup.exe,
;                LICENSE and README.md
;   OUTFILE      the installer to write
;   COPYRIGHT    the copyright line from LICENSE
;
; Per user, no administrator rights, into a fixed folder, so Claude's
; registration survives updates (issue #89):
; - a first install starts the setup wizard (also when silent, as winget runs
;   it; /NOSETUP leaves it out);
; - an update only replaces the program: no questions, settings kept;
; - uninstalling removes the program and its entry in Claude, and the
;   settings and stored password only if the user ticks that.

Unicode true
ManifestDPIAware true
SetCompressor /SOLID lzma

!include MUI2.nsh
!include LogicLib.nsh
!include FileFunc.nsh
!include x64.nsh

!define PRODUCT "Proton Mail MCP"
!define PUBLISHER "codeby.se"
!define UNINSTKEY "Software\Microsoft\Windows\CurrentVersion\Uninstall\ProtonMailMCP"
!define REPO "https://github.com/dvalfrid/proton-mail-mcp"
!define SERVER "proton-mail-mcp.exe"
!define WIZARD "proton-mail-mcp-setup.exe"

Name "${PRODUCT}"
OutFile "${OUTFILE}"
RequestExecutionLevel user
InstallDir "$LOCALAPPDATA\Programs\proton-mail-mcp"
BrandingText "${PRODUCT} ${VERSION}"

VIProductVersion "${FILEVERSION}"
VIFileVersion "${FILEVERSION}"
VIAddVersionKey "ProductName" "${PRODUCT}"
VIAddVersionKey "FileDescription" "${PRODUCT} installer"
VIAddVersionKey "ProductVersion" "${VERSION}"
VIAddVersionKey "FileVersion" "${VERSION}"
VIAddVersionKey "CompanyName" "${PUBLISHER}"
VIAddVersionKey "LegalCopyright" "${COPYRIGHT}"

Var Update   ; 1 when an earlier version is installed
Var NoSetup  ; 1 with /NOSETUP

!define MUI_ICON "../assets/icon.ico"
!define MUI_UNICON "../assets/icon.ico"
!define MUI_ABORTWARNING

!define MUI_WELCOMEPAGE_TEXT "This installs ${PRODUCT} ${VERSION}, which lets Claude read and organise your Proton Mail through Proton Mail Bridge on this computer.$\r$\n$\r$\nIt installs for you only and doesn't need administrator rights. Afterwards, setup finds Bridge and asks for your Bridge password."
!define MUI_PAGE_CUSTOMFUNCTION_PRE SkipOnUpdate
!insertmacro MUI_PAGE_WELCOME
!insertmacro MUI_PAGE_INSTFILES
!define MUI_FINISHPAGE_RUN "$INSTDIR\${WIZARD}"
!define MUI_FINISHPAGE_RUN_TEXT "Set up ${PRODUCT} now"
!define MUI_PAGE_CUSTOMFUNCTION_SHOW FinishShow
!insertmacro MUI_PAGE_FINISH

!insertmacro MUI_UNPAGE_CONFIRM
!insertmacro MUI_UNPAGE_COMPONENTS
!insertmacro MUI_UNPAGE_INSTFILES

!insertmacro MUI_LANGUAGE "English"

Function .onInit
  !if "${ARCH}" == "arm64"
    ${IfNot} ${IsNativeARM64}
      MessageBox MB_ICONSTOP "This installer is for Windows on ARM. Download the x64 one." /SD IDOK
      Abort
    ${EndIf}
  !endif
  StrCpy $Update 0
  ReadRegStr $0 HKCU "${UNINSTKEY}" "InstallLocation"
  ${If} $0 != ""
    StrCpy $INSTDIR $0
  ${EndIf}
  ${If} ${FileExists} "$INSTDIR\${SERVER}"
    StrCpy $Update 1
  ${EndIf}
  ${GetParameters} $0
  ClearErrors
  ${GetOptions} $0 "/NOSETUP" $1
  ${If} ${Errors}
    StrCpy $NoSetup 0
  ${Else}
    StrCpy $NoSetup 1
  ${EndIf}
FunctionEnd

Function SkipOnUpdate
  ${If} $Update == 1
    Abort
  ${EndIf}
FunctionEnd

; On an update the finish page only says it is done: setup has run before.
Function FinishShow
  ${If} $Update == 1
    ShowWindow $mui.FinishPage.Run ${SW_HIDE}
    SendMessage $mui.FinishPage.Run ${BM_SETCHECK} ${BST_UNCHECKED} 0
    SendMessage $mui.FinishPage.Text ${WM_SETTEXT} 0 "STR:${PRODUCT} is updated to ${VERSION}. Your settings are kept.$\r$\n$\r$\nRestart Claude Desktop and Claude Code to use the new version."
  ${EndIf}
FunctionEnd

; Replaces a program that may be running (Claude keeps the server open): a
; running exe can't be overwritten, but it can be renamed. Claude can still
; run copies from earlier updates too (issue #139), so the exe is renamed to
; a free name (NAME.old0, .old1, …). Old copies are removed now if they
; aren't running, or on the next update.
!macro Replace NAME
  Delete "$INSTDIR\${NAME}.old*"
  ${If} ${FileExists} "$INSTDIR\${NAME}"
    StrCpy $R0 0
    ${DoWhile} ${FileExists} "$INSTDIR\${NAME}.old$R0"
      IntOp $R0 $R0 + 1
    ${Loop}
    Rename "$INSTDIR\${NAME}" "$INSTDIR\${NAME}.old$R0"
  ${EndIf}
  File "${SRCDIR}\${NAME}"
  Delete "$INSTDIR\${NAME}.old*"
!macroend

Section "Install"
  SetOutPath "$INSTDIR"
  !insertmacro Replace "${SERVER}"
  !insertmacro Replace "${WIZARD}"
  File "${SRCDIR}\LICENSE"
  File "${SRCDIR}\README.md"
  WriteUninstaller "$INSTDIR\uninstall.exe"

  CreateShortcut "$SMPROGRAMS\${PRODUCT} Setup.lnk" "$INSTDIR\${WIZARD}" "" "$INSTDIR\${WIZARD}" 0 "" "" "Set up ${PRODUCT}: Bridge, password, Claude"
  ; check prints to the console, which stays open to read it.
  CreateShortcut "$SMPROGRAMS\${PRODUCT} Check.lnk" "$WINDIR\System32\cmd.exe" '/k ""$INSTDIR\${SERVER}" check"' "$INSTDIR\${WIZARD}" 0 "" "" "Test the connection to Proton Mail Bridge"

  WriteRegStr HKCU "${UNINSTKEY}" "DisplayName" "${PRODUCT}"
  WriteRegStr HKCU "${UNINSTKEY}" "DisplayVersion" "${VERSION}"
  WriteRegStr HKCU "${UNINSTKEY}" "Publisher" "${PUBLISHER}"
  WriteRegStr HKCU "${UNINSTKEY}" "DisplayIcon" "$INSTDIR\${WIZARD},0"
  WriteRegStr HKCU "${UNINSTKEY}" "InstallLocation" "$INSTDIR"
  WriteRegStr HKCU "${UNINSTKEY}" "UninstallString" '"$INSTDIR\uninstall.exe"'
  WriteRegStr HKCU "${UNINSTKEY}" "QuietUninstallString" '"$INSTDIR\uninstall.exe" /S'
  WriteRegStr HKCU "${UNINSTKEY}" "URLInfoAbout" "${REPO}"
  WriteRegStr HKCU "${UNINSTKEY}" "HelpLink" "${REPO}/blob/main/docs/troubleshooting.md"
  WriteRegDWORD HKCU "${UNINSTKEY}" "NoModify" 1
  WriteRegDWORD HKCU "${UNINSTKEY}" "NoRepair" 1
  ${GetSize} "$INSTDIR" "/S=0K" $0 $1 $2
  WriteRegDWORD HKCU "${UNINSTKEY}" "EstimatedSize" $0
SectionEnd

; A silent first install (winget) starts setup too; the finish page does it
; otherwise.
Function .onInstSuccess
  ${If} ${Silent}
  ${AndIf} $Update == 0
  ${AndIf} $NoSetup == 0
    Exec '"$INSTDIR\${WIZARD}"'
  ${EndIf}
FunctionEnd

Section /o "un.Settings and saved Bridge password" SecUnSettings
  ; Done by -un.Program below, when this is ticked.
SectionEnd

Section "-un.Program" SecUnProgram
  ; Removes proton-mail-mcp from Claude where it starts this copy, and with
  ; the box ticked also the settings and the stored password.
  ${If} ${SectionIsSelected} ${SecUnSettings}
    ExecWait '"$INSTDIR\${WIZARD}" -uninstall -remove-settings'
  ${Else}
    ExecWait '"$INSTDIR\${WIZARD}" -uninstall'
  ${EndIf}

  ; Claude may still be running the server.
  retry:
  ClearErrors
  Delete "$INSTDIR\${SERVER}"
  ${If} ${Errors}
    MessageBox MB_RETRYCANCEL|MB_ICONEXCLAMATION "Claude is still using ${PRODUCT}. Close Claude Desktop and Claude Code, then choose Retry." /SD IDCANCEL IDRETRY retry
  ${EndIf}
  Delete "$INSTDIR\${SERVER}.old*"
  Delete "$INSTDIR\${WIZARD}"
  Delete "$INSTDIR\${WIZARD}.old*"
  Delete "$INSTDIR\LICENSE"
  Delete "$INSTDIR\README.md"
  Delete "$INSTDIR\uninstall.exe"
  RMDir "$INSTDIR"

  Delete "$SMPROGRAMS\${PRODUCT} Setup.lnk"
  Delete "$SMPROGRAMS\${PRODUCT} Check.lnk"
  DeleteRegKey HKCU "${UNINSTKEY}"
SectionEnd

!insertmacro MUI_UNFUNCTION_DESCRIPTION_BEGIN
  !insertmacro MUI_DESCRIPTION_TEXT ${SecUnSettings} "Also remove your settings, Bridge's pinned certificate and the Bridge password saved in Windows Credential Manager. Leave it unticked to keep them for a later install."
!insertmacro MUI_UNFUNCTION_DESCRIPTION_END
